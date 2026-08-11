// Package fiado serves the caderninho to the dashboard. Read-only, on purpose:
// registering a purchase and settling one are conversations with the bot, where
// the client's name is reconciled before anything is written. A form here would
// happily create "joão", "João Silva" and "Joao S." as three debtors, which is
// exactly what makes the caderninho lie for less (ADR-027 §5).
//
// It is its own package rather than three more handlers in internal/finance:
// the caderninho is a system apart, and nothing here touches the ledger.
package fiado

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/domain"
	pkgfiado "github.com/emerson/emerbot/packages/fiado"
)

// Reader is the slice of the caderninho these endpoints use: the reads, and
// none of the writes (ADR-014 — a consumer declares the interface it consumes).
type Reader interface {
	Debtor(ctx context.Context, userID, client string) (pkgfiado.Debtor, error)
	ListDebtors(ctx context.Context, userID string) ([]pkgfiado.Debtor, error)
	DayMovements(ctx context.Context, userID string, date domain.CalendarDate, page pkgfiado.Page) (pkgfiado.MovementPage, error)
	ClientMovements(ctx context.Context, userID, client string, page pkgfiado.Page) (pkgfiado.MovementPage, error)
}

type Handler struct {
	store Reader
	loc   *time.Location
}

// NewHandler builds the handler. loc is the calendar "hoje" is resolved in when
// ageing a debt: the pharmacy's day, not the Lambda's UTC one — after 21h in
// Brazil those are different days, and the browser must not do this arithmetic
// either.
func NewHandler(store Reader, loc *time.Location) *Handler {
	if loc == nil {
		loc = time.UTC
	}
	return &Handler{store: store, loc: loc}
}

// devedorResponse is one line of the caderninho. Amounts are centavos and dates
// are "YYYY-MM-DD", like the rest of this API.
type devedorResponse struct {
	Cliente string `json:"cliente"`
	Nome    string `json:"nome"`
	// Saldo is positive for a debt, zero for a settled account and negative for
	// the client's credit. All three happen and none is an error, so the field
	// carries the sign and the screen names the three cases.
	Saldo int64 `json:"saldo"`
	// Desde is the day the balance left zero — null once there is no debt to
	// date from.
	Desde *string `json:"desde"`
	// DiasEmAberto is counted here, never in the browser: the day that matters
	// is the pharmacy's. Null when there is nothing to age, including a debt
	// that started today (ADR-017 — N starts at 1).
	DiasEmAberto *int `json:"dias_em_aberto"`
}

type caderninhoResponse struct {
	Devedores []devedorResponse `json:"devedores"`
	// TotalEmAberto sums only what is owed. A client's credit is not a discount
	// on what other people owe, so netting it in would understate the book.
	TotalEmAberto int64 `json:"total_em_aberto"`
	Count         int   `json:"count"`
}

// movimentoResponse is one line of a timeline. There is no "tipo": the sign of
// valor is the type, and the UI reads it (ADR-027 §3).
type movimentoResponse struct {
	ID        string `json:"id"`
	Cliente   string `json:"cliente"`
	Nome      string `json:"nome"`
	Valor     int64  `json:"valor"`
	Data      string `json:"data"`
	Descricao string `json:"descricao"`
}

// movimentosResponse is a page of a timeline. NextCursor is absent when the
// timeline ended — that absence is what ends the pagination — and a page that
// was cut says so out loud, with a warning meant to be rendered (ADR-015).
type movimentosResponse struct {
	Movimentos []movimentoResponse `json:"movimentos"`
	Count      int                 `json:"count"`
	NextCursor string              `json:"next_cursor,omitempty"`
	Truncated  bool                `json:"truncated"`
	Warning    string              `json:"warning,omitempty"`
}

// List handles GET /fiado — the caderninho, biggest debt first.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	debtors, err := h.store.ListDebtors(r.Context(), claims.UserID)
	if err != nil {
		httpx.Error(w, "failed to list caderninho", http.StatusInternalServerError)
		return
	}

	today := domain.NewCalendarDate(time.Now().In(h.loc))
	out := caderninhoResponse{Devedores: make([]devedorResponse, 0, len(debtors))}
	for _, d := range debtors {
		out.Devedores = append(out.Devedores, h.devedor(d, today))
		if d.Balance > 0 {
			out.TotalEmAberto += d.Balance
		}
	}
	// Who owes most, first. The store returns the key's order (by slug), which
	// is the order that keeps its two implementations honest — the useful order
	// for a screen is a presentation choice and belongs here.
	sort.SliceStable(out.Devedores, func(i, j int) bool {
		return out.Devedores[i].Saldo > out.Devedores[j].Saldo
	})
	out.Count = len(out.Devedores)

	httpx.OK(w, out)
}

// Get handles GET /fiado/{cliente} — one account, for a page opened by URL. A
// 404 means that person is not in the caderninho, not that the read failed.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	client := r.PathValue("cliente")
	if client == "" {
		httpx.Error(w, "cliente is required", http.StatusBadRequest)
		return
	}

	d, err := h.store.Debtor(r.Context(), claims.UserID, client)
	if errors.Is(err, pkgfiado.ErrDebtorNotFound) {
		httpx.Error(w, "cliente não está no caderninho", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.Error(w, "failed to read cliente", http.StatusInternalServerError)
		return
	}

	httpx.OK(w, h.devedor(d, domain.NewCalendarDate(time.Now().In(h.loc))))
}

// ClientMovements handles GET /fiado/{cliente}/movimentos — one person's
// statement, most recent first, paginated by the cursor the store hands back.
func (h *Handler) ClientMovements(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	client := r.PathValue("cliente")
	if client == "" {
		httpx.Error(w, "cliente is required", http.StatusBadRequest)
		return
	}

	page, err := h.store.ClientMovements(r.Context(), claims.UserID, client, requestedPage(r))
	if err != nil {
		// A cursor the store refuses is a bad request, not an outage: it came
		// from the caller and names another timeline.
		httpx.Error(w, "failed to read movimentos", http.StatusInternalServerError)
		return
	}
	httpx.OK(w, movimentos(page))
}

// DayMovements handles GET /fiado/movimentos?date=YYYY-MM-DD — the caderninho
// on one day, across every client.
func (h *Handler) DayMovements(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Required, and malformed input is an error rather than a silent fallback
	// to today: "no movement on that day" and "you typed the date wrong" must
	// not render the same.
	date, err := httpx.Day(r, "date")
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	page, err := h.store.DayMovements(r.Context(), claims.UserID, date, requestedPage(r))
	if err != nil {
		httpx.Error(w, "failed to read movimentos", http.StatusInternalServerError)
		return
	}
	httpx.OK(w, movimentos(page))
}

func (h *Handler) devedor(d pkgfiado.Debtor, today domain.CalendarDate) devedorResponse {
	out := devedorResponse{
		Cliente:      d.Client,
		Nome:         d.Name,
		Saldo:        d.Balance,
		DiasEmAberto: pkgfiado.DaysOpen(d, today),
	}
	if d.Since != nil {
		since := d.Since.String()
		out.Desde = &since
	}
	return out
}

// requestedPage reads limit and cursor. The cursor is opaque and goes through
// untouched — interpreting it here would be a second implementation of the
// store's pagination.
func requestedPage(r *http.Request) pkgfiado.Page {
	q := r.URL.Query()
	page := pkgfiado.Page{Limit: pkgfiado.DefaultPageLimit, Cursor: q.Get("cursor")}
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			page.Limit = pkgfiado.ClampLimit(n)
		}
	}
	return page
}

func movimentos(page pkgfiado.MovementPage) movimentosResponse {
	out := movimentosResponse{
		Movimentos: make([]movimentoResponse, 0, len(page.Movements)),
		NextCursor: page.NextCursor,
		Truncated:  page.NextCursor != "",
	}
	for _, m := range page.Movements {
		out.Movimentos = append(out.Movimentos, movimentoResponse{
			ID:        m.ID,
			Cliente:   m.Client,
			Nome:      m.Name,
			Valor:     m.Amount,
			Data:      m.Date.String(),
			Descricao: m.Description,
		})
	}
	out.Count = len(out.Movimentos)
	if out.Truncated {
		out.Warning = fmt.Sprintf(
			"Mostrando %d movimentos; há mais para carregar.", out.Count,
		)
	}
	return out
}
