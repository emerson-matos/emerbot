package fiado

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfiado "github.com/emerson/emerbot/packages/fiado"
)

// These tests drive each handler as an http.Handler over the in-memory
// caderninho, so the path values, the query params and the JSON envelope are
// checked together — the same path the dashboard hits, and the same field
// names it reads.

const testUser = "shared-ledger"

func newHandler(t *testing.T) (*Handler, *pkgfiado.InMemoryStore) {
	t.Helper()
	store := pkgfiado.NewInMemoryStore()
	return NewHandler(store, time.UTC), store
}

func authed(target string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	claims := apiauth.Claims{UserID: testUser, Subject: "cognito-sub"}
	return r.WithContext(apiauth.WithClaims(r.Context(), claims))
}

func withClient(r *http.Request, cliente string) *http.Request {
	r.SetPathValue("cliente", cliente)
	return r
}

func run(h http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var got T
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	return got
}

func day(t *testing.T, s string) domain.CalendarDate {
	t.Helper()
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return d
}

func record(t *testing.T, store *pkgfiado.InMemoryStore, name, date string, amount int64) pkgfiado.Movement {
	t.Helper()
	m, err := pkgfiado.NewMovement(testUser, name, amount, day(t, date), "")
	if err != nil {
		t.Fatalf("new movement: %v", err)
	}
	if _, err := store.Record(context.Background(), m); err != nil {
		t.Fatalf("record: %v", err)
	}
	return m
}

func TestListReturnsTheCaderninhoBiggestDebtFirst(t *testing.T) {
	h, store := newHandler(t)
	record(t, store, "Ana", "2026-08-01", 1000)
	record(t, store, "João Silva", "2026-08-01", 34000)
	record(t, store, "Zeca", "2026-08-01", 5000)

	w := run(h.List, authed("/fiado"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	got := decode[caderninhoResponse](t, w)

	if got.Count != 3 {
		t.Fatalf("count = %d, want 3", got.Count)
	}
	if got.TotalEmAberto != 40000 {
		t.Fatalf("total_em_aberto = %d, want 40000", got.TotalEmAberto)
	}
	order := []string{got.Devedores[0].Cliente, got.Devedores[1].Cliente, got.Devedores[2].Cliente}
	want := []string{"joao_silva", "zeca", "ana"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("ordem = %v, want %v (quem deve mais primeiro)", order, want)
		}
	}
	if got.Devedores[0].Nome != "João Silva" {
		t.Fatalf("nome = %q, want %q — a tela mostra o nome digitado, não o slug", got.Devedores[0].Nome, "João Silva")
	}
}

// A client's credit is not a discount on what other people owe. Netting it into
// the total would understate the book, and the total is the one number the page
// puts at the top.
func TestListDoesNotNetCreditAgainstWhatIsOwed(t *testing.T) {
	h, store := newHandler(t)
	record(t, store, "Ana", "2026-08-01", 10000)
	record(t, store, "Zeca", "2026-08-01", -3000)

	got := decode[caderninhoResponse](t, run(h.List, authed("/fiado")))
	if got.TotalEmAberto != 10000 {
		t.Fatalf("total_em_aberto = %d, want 10000", got.TotalEmAberto)
	}
	// The credit is still listed — it is a real account, just not a debt.
	if got.Count != 2 {
		t.Fatalf("count = %d, want 2", got.Count)
	}
}

// Nobody owing is a good answer, not an empty screen: the list has to be an
// empty array rather than a null the browser has to guard against.
func TestListAnswersAnEmptyCaderninho(t *testing.T) {
	h, _ := newHandler(t)

	w := run(h.List, authed("/fiado"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	raw := decode[map[string]any](t, w)
	list, ok := raw["devedores"].([]any)
	if !ok {
		t.Fatalf("devedores = %#v, want an array", raw["devedores"])
	}
	if len(list) != 0 || raw["count"].(float64) != 0 || raw["total_em_aberto"].(float64) != 0 {
		t.Fatalf("caderninho vazio = %#v", raw)
	}
}

func TestGetAgesTheDebtOnTheServer(t *testing.T) {
	h, store := newHandler(t)
	record(t, store, "João Silva", "2026-08-01", 34000)

	w := run(h.Get, withClient(authed("/fiado/joao_silva"), "joao_silva"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	got := decode[devedorResponse](t, w)

	if got.Cliente != "joao_silva" || got.Saldo != 34000 {
		t.Fatalf("devedor = %+v", got)
	}
	if got.Desde == nil || *got.Desde != "2026-08-01" {
		t.Fatalf("desde = %v, want 2026-08-01", got.Desde)
	}
	// The count itself depends on the real clock, so what is asserted is that
	// the server produced one rather than leaving it to the browser.
	if got.DiasEmAberto == nil {
		t.Fatal("dias_em_aberto veio null numa dívida aberta — o navegador não pode contar isso")
	}
}

func TestGetSaysWhenAnAccountIsSettled(t *testing.T) {
	h, store := newHandler(t)
	record(t, store, "João", "2026-08-01", 4000)
	record(t, store, "João", "2026-08-05", -4000)

	got := decode[devedorResponse](t, run(h.Get, withClient(authed("/fiado/joao"), "joao")))
	if got.Saldo != 0 {
		t.Fatalf("saldo = %d, want 0", got.Saldo)
	}
	if got.Desde != nil || got.DiasEmAberto != nil {
		t.Fatalf("conta quitada com desde=%v dias=%v, want ambos null", got.Desde, got.DiasEmAberto)
	}
}

// 404 means "that person is not in the caderninho", which the page has its own
// state for — not "the read failed".
func TestGetIsNotFoundForSomebodyNotInTheBook(t *testing.T) {
	h, _ := newHandler(t)

	w := run(h.Get, withClient(authed("/fiado/ninguem"), "ninguem"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestClientMovementsAreMostRecentFirst(t *testing.T) {
	h, store := newHandler(t)
	record(t, store, "João", "2026-08-01", 4000)
	record(t, store, "João", "2026-08-05", -1500)

	got := decode[movimentosResponse](t, run(h.ClientMovements,
		withClient(authed("/fiado/joao/movimentos"), "joao")))

	if got.Count != 2 {
		t.Fatalf("count = %d, want 2", got.Count)
	}
	if got.Movimentos[0].Data != "2026-08-05" {
		t.Fatalf("primeiro movimento = %s, want 2026-08-05", got.Movimentos[0].Data)
	}
	// The sign is the type: a payment is the same shape with a negative valor.
	if got.Movimentos[0].Valor != -1500 {
		t.Fatalf("valor = %d, want -1500", got.Movimentos[0].Valor)
	}
	if got.Truncated || got.NextCursor != "" {
		t.Fatalf("lista completa marcada como cortada: %+v", got)
	}
}

// A cut list never goes out quietly (ADR-015): the cursor is what continues it
// and the warning is what the screen says while it has not.
func TestClientMovementsPaginateAndSayTheyWereCut(t *testing.T) {
	h, store := newHandler(t)
	for _, d := range []string{"2026-08-01", "2026-08-02", "2026-08-03"} {
		record(t, store, "João", d, 1000)
	}

	first := decode[movimentosResponse](t, run(h.ClientMovements,
		withClient(authed("/fiado/joao/movimentos?limit=2"), "joao")))
	if first.Count != 2 {
		t.Fatalf("count = %d, want 2", first.Count)
	}
	if !first.Truncated || first.NextCursor == "" || first.Warning == "" {
		t.Fatalf("página cortada saiu calada: %+v", first)
	}

	second := decode[movimentosResponse](t, run(h.ClientMovements,
		withClient(authed("/fiado/joao/movimentos?limit=2&cursor="+first.NextCursor), "joao")))
	if second.Count != 1 {
		t.Fatalf("segunda página tem %d movimentos, want 1", second.Count)
	}
	if second.Truncated || second.NextCursor != "" {
		t.Fatalf("a última página ainda diz que há mais: %+v", second)
	}
	if second.Movimentos[0].Data != "2026-08-01" {
		t.Fatalf("último movimento = %s, want 2026-08-01", second.Movimentos[0].Data)
	}
}

func TestDayMovementsRequireADate(t *testing.T) {
	h, store := newHandler(t)
	record(t, store, "João", "2026-08-01", 4000)

	for _, target := range []string{"/fiado/movimentos", "/fiado/movimentos?date=ontem"} {
		w := run(h.DayMovements, authed(target))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 — uma data que não dá para ler não pode virar hoje", target, w.Code)
		}
	}
}

func TestDayMovementsReturnTheWholeDay(t *testing.T) {
	h, store := newHandler(t)
	record(t, store, "João", "2026-08-01", 4000)
	record(t, store, "Ana", "2026-08-02", 2000)
	record(t, store, "João", "2026-08-02", -500)

	got := decode[movimentosResponse](t, run(h.DayMovements, authed("/fiado/movimentos?date=2026-08-02")))
	if got.Count != 2 {
		t.Fatalf("count = %d, want 2", got.Count)
	}
	for _, m := range got.Movimentos {
		if m.Data != "2026-08-02" {
			t.Fatalf("movimento de outro dia na listagem: %+v", m)
		}
	}
}

func TestEndpointsRefuseAnAnonymousRequest(t *testing.T) {
	h, _ := newHandler(t)
	cases := map[string]struct {
		handler http.HandlerFunc
		target  string
	}{
		"list":    {h.List, "/fiado"},
		"get":     {h.Get, "/fiado/joao"},
		"cliente": {h.ClientMovements, "/fiado/joao/movimentos"},
		"do dia":  {h.DayMovements, "/fiado/movimentos?date=2026-08-02"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			w := run(tc.handler, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
		})
	}
}

// One user's caderninho is not another's.
func TestEndpointsAreScopedToTheCaller(t *testing.T) {
	h, store := newHandler(t)
	other, err := pkgfiado.NewMovement("someone-else", "João", 9900, day(t, "2026-08-01"), "")
	if err != nil {
		t.Fatalf("new movement: %v", err)
	}
	if _, err := store.Record(context.Background(), other); err != nil {
		t.Fatalf("record: %v", err)
	}
	record(t, store, "João", "2026-08-01", 4000)

	got := decode[devedorResponse](t, run(h.Get, withClient(authed("/fiado/joao"), "joao")))
	if got.Saldo != 4000 {
		t.Fatalf("saldo = %d, want 4000 — o caderninho de outro usuário vazou", got.Saldo)
	}
}
