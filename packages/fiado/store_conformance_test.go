package fiado

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore/dynamotest"
)

// The caderninho has two Store implementations and the caller picks one by
// configuration, so anything they answer differently is a bug that only shows
// up in one environment (ADR-014). This suite runs the same scenario against
// both.
//
// It matters more here than in most places: the DynamoDB store keeps a
// debtor's balance with a blind atomic ADD and repairs "desde" afterwards,
// while the in-memory one computes both directly. Those are two very different
// mechanisms for one rule, and only a shared scenario keeps them the same rule.

const testTable = "emerbot-fiado-test"

// newFakeTable is shaped like the deployed financial-entries table, GSIs
// included. The caderninho writes no GSI attributes, so the indexes must come
// back empty — see TestCaderninhoStaysOutOfTheIndexes.
func newFakeTable(pageSize int) *dynamotest.Table {
	return dynamotest.New(dynamotest.Config{
		Name: testTable,
		Key:  dynamotest.Key{Hash: "PK", Range: "SK"},
		GSIs: map[string]dynamotest.Key{
			"GSI2-Status":   {Hash: "GSI2PK", Range: "GSI2SK"},
			"GSI1-Category": {Hash: "GSI1PK", Range: "GSI1SK"},
		},
		PageSize: pageSize,
	})
}

// eachStore runs fn against every Store implementation.
func eachStore(t *testing.T, fn func(t *testing.T, s Store)) {
	t.Helper()
	impls := map[string]func() Store{
		"inmemory": func() Store { return NewInMemoryStore() },
		"dynamodb": func() Store {
			return NewDynamoDBStoreWithClient(newFakeTable(0), testTable)
		},
	}
	for name, build := range impls {
		t.Run(name, func(t *testing.T) { fn(t, build()) })
	}
}

const user = "u1"

func day(t *testing.T, s string) domain.CalendarDate {
	t.Helper()
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

// mov builds a movement with a caller-chosen id, because a test that asserts
// ordering cannot depend on a random ULID.
func mov(t *testing.T, id, name, date string, amount int64) Movement {
	t.Helper()
	m, err := NewMovement(user, name, amount, day(t, date), "")
	if err != nil {
		t.Fatalf("new movement: %v", err)
	}
	m.ID = id
	return m
}

func record(t *testing.T, s Store, m Movement) Debtor {
	t.Helper()
	d, err := s.Record(context.Background(), m)
	if err != nil {
		t.Fatalf("record %s %d: %v", m.Client, m.Amount, err)
	}
	return d
}

// assertBalance checks the invariant the whole design rests on: the latest is
// only a cache, so it must equal the sum of the movements it summarises.
func assertBalance(t *testing.T, s Store, client string, want int64) {
	t.Helper()
	ctx := context.Background()

	d, err := s.Debtor(ctx, user, client)
	if err != nil {
		t.Fatalf("debtor %s: %v", client, err)
	}
	if d.Balance != want {
		t.Fatalf("saldo de %s = %d, want %d", client, d.Balance, want)
	}

	page, err := s.ClientMovements(ctx, user, client, Page{})
	if err != nil {
		t.Fatalf("client movements %s: %v", client, err)
	}
	if got := Sum(page.Movements); got != d.Balance {
		t.Fatalf("soma dos movimentos de %s = %d, mas o latest diz %d — o latest é cache, divergir é bug", client, got, d.Balance)
	}
}

func sinceOf(d Debtor) string {
	if d.Since == nil {
		return ""
	}
	return d.Since.String()
}

func TestStoresAgreeOnASequenceOfPurchasesAndPayments(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		record(t, s, mov(t, "m1", "João Silva", "2026-08-01", 4000))
		record(t, s, mov(t, "m2", "João Silva", "2026-08-03", 3000))
		last := record(t, s, mov(t, "m3", "João Silva", "2026-08-05", -5000))

		if last.Balance != 2000 {
			t.Fatalf("saldo = %d, want 2000", last.Balance)
		}
		if last.Name != "João Silva" {
			t.Fatalf("nome = %q, want %q", last.Name, "João Silva")
		}
		assertBalance(t, s, "joao_silva", 2000)

		// The sign is the only type there is: what was taken and what was paid
		// come out of the same field.
		page, err := s.ClientMovements(context.Background(), user, "joao_silva", Page{})
		if err != nil {
			t.Fatalf("client movements: %v", err)
		}
		taken, paid := Totals(page.Movements)
		if taken != 7000 || paid != 5000 {
			t.Fatalf("comprado/pago = %d/%d, want 7000/5000", taken, paid)
		}
	})
}

func TestStoresAgreeOnDesdeCrossingZero(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		opened := record(t, s, mov(t, "m1", "João", "2026-08-01", 4000))
		if got := sinceOf(opened); got != "2026-08-01" {
			t.Fatalf("desde = %q, want 2026-08-01 (o dia em que o saldo saiu de zero)", got)
		}

		// A second purchase does not move it: "desde" is when the debt started,
		// not when it last grew.
		grew := record(t, s, mov(t, "m2", "João", "2026-08-04", 1000))
		if got := sinceOf(grew); got != "2026-08-01" {
			t.Fatalf("desde após nova compra = %q, want 2026-08-01", got)
		}

		settled := record(t, s, mov(t, "m3", "João", "2026-08-09", -5000))
		if settled.Balance != 0 || settled.Since != nil {
			t.Fatalf("quitado: saldo=%d desde=%q, want 0 e sem desde", settled.Balance, sinceOf(settled))
		}

		// And it starts again from the new purchase, not from the old debt.
		again := record(t, s, mov(t, "m4", "João", "2026-08-20", 2500))
		if got := sinceOf(again); got != "2026-08-20" {
			t.Fatalf("desde após voltar a dever = %q, want 2026-08-20", got)
		}
	})
}

// A client who overpays is in credit, not in debt. Nothing may date a debt that
// is not there — "em aberto há 30 dias" over money the pharmacy owes *them* is
// the caderninho lying in the direction that makes somebody stop using it.
func TestStoresAgreeThatCreditIsNotAnOpenDebt(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		record(t, s, mov(t, "m1", "Maria", "2026-07-01", 3000))
		credit := record(t, s, mov(t, "m2", "Maria", "2026-08-01", -5000))

		if credit.Balance != -2000 {
			t.Fatalf("saldo = %d, want -2000 (crédito do cliente)", credit.Balance)
		}
		if credit.Since != nil {
			t.Fatalf("desde = %q em conta com crédito, want vazio", sinceOf(credit))
		}
		if d := DaysOpen(credit, day(t, "2026-08-31")); d != nil {
			t.Fatalf("dias em aberto = %d em conta com crédito, want nenhum", *d)
		}
		assertBalance(t, s, "maria", -2000)

		// Buying again re-opens the account on the new purchase's day, and the
		// credit is consumed by the balance rather than by a rule of its own.
		back := record(t, s, mov(t, "m3", "Maria", "2026-09-02", 5000))
		if back.Balance != 3000 {
			t.Fatalf("saldo = %d, want 3000", back.Balance)
		}
		if got := sinceOf(back); got != "2026-09-02" {
			t.Fatalf("desde = %q, want 2026-09-02 (a compra nova, não a dívida antiga)", got)
		}
	})
}

func TestStoresAgreeOnDeletingAMovement(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		record(t, s, mov(t, "m1", "João", "2026-08-01", 4000))
		wrong := mov(t, "m2", "João", "2026-08-02", 9900)
		record(t, s, wrong)
		assertBalance(t, s, "joao", 13900)

		// Correcting is deleting the wrong line, never posting a compensating
		// one: an "adjustment" entered as a negative would be counted as money
		// the client paid.
		after, err := s.Delete(ctx, user, wrong.Ref())
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		if after.Balance != 4000 {
			t.Fatalf("saldo após apagar = %d, want 4000", after.Balance)
		}
		assertBalance(t, s, "joao", 4000)

		// Both copies are gone: neither timeline may still show it.
		clientPage, err := s.ClientMovements(ctx, user, "joao", Page{})
		if err != nil {
			t.Fatalf("client movements: %v", err)
		}
		if len(clientPage.Movements) != 1 {
			t.Fatalf("extrato do cliente tem %d movimentos, want 1", len(clientPage.Movements))
		}
		dayPage, err := s.DayMovements(ctx, user, day(t, "2026-08-02"), Page{})
		if err != nil {
			t.Fatalf("day movements: %v", err)
		}
		if len(dayPage.Movements) != 0 {
			t.Fatalf("o dia ainda mostra %d movimentos, want 0 — a cópia por dia não foi apagada", len(dayPage.Movements))
		}
	})
}

// Deleting the payment that had settled an account re-opens it, and the day it
// last left zero is no longer written down anywhere — it has to come back from
// the movements, which are the truth.
func TestStoresAgreeOnADeleteThatReopensASettledAccount(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		record(t, s, mov(t, "m1", "João", "2026-08-01", 4000))
		payment := mov(t, "m2", "João", "2026-08-10", -4000)
		settled := record(t, s, payment)
		if settled.Since != nil {
			t.Fatalf("desde = %q com a conta quitada, want vazio", sinceOf(settled))
		}

		reopened, err := s.Delete(ctx, user, payment.Ref())
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		if reopened.Balance != 4000 {
			t.Fatalf("saldo = %d, want 4000", reopened.Balance)
		}
		if got := sinceOf(reopened); got != "2026-08-01" {
			t.Fatalf("desde = %q, want 2026-08-01 — recuperado dos movimentos", got)
		}

		// And the stored latest agrees with what Delete returned.
		stored, err := s.Debtor(ctx, user, "joao")
		if err != nil {
			t.Fatalf("debtor: %v", err)
		}
		if sinceOf(stored) != "2026-08-01" {
			t.Fatalf("desde gravado = %q, want 2026-08-01", sinceOf(stored))
		}
	})
}

func TestStoresAgreeOnAMissingMovement(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		record(t, s, mov(t, "m1", "João", "2026-08-01", 4000))

		_, err := s.Delete(context.Background(), user, Ref{Client: "joao", Date: day(t, "2026-08-01"), ID: "nope"})
		if !errors.Is(err, ErrMovementNotFound) {
			t.Fatalf("delete de movimento inexistente: err = %v, want ErrMovementNotFound", err)
		}
	})
}

func TestStoresAgreeOnAnUnknownDebtor(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		record(t, s, mov(t, "m1", "João", "2026-08-01", 4000))

		if _, err := s.Debtor(ctx, user, "maria"); !errors.Is(err, ErrDebtorNotFound) {
			t.Fatalf("debtor desconhecido: err = %v, want ErrDebtorNotFound", err)
		}
		// A settled client is still in the book: they are somebody the name
		// reconciliation has to be able to find.
		record(t, s, mov(t, "m2", "João", "2026-08-02", -4000))
		if _, err := s.Debtor(ctx, user, "joao"); err != nil {
			t.Fatalf("cliente quitado sumiu do caderninho: %v", err)
		}
	})
}

func TestStoresAgreeOnTheCaderninhoListing(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		record(t, s, mov(t, "m1", "João Silva", "2026-08-01", 4000))
		record(t, s, mov(t, "m2", "Ana", "2026-08-01", 7000))
		record(t, s, mov(t, "m3", "João Silva", "2026-08-02", -1000))

		debtors, err := s.ListDebtors(context.Background(), user)
		if err != nil {
			t.Fatalf("list debtors: %v", err)
		}
		// Only the latests, one per client — never a movement, and never one
		// row per movement.
		if len(debtors) != 2 {
			t.Fatalf("caderninho tem %d linhas, want 2: %+v", len(debtors), debtors)
		}
		if debtors[0].Client != "ana" || debtors[1].Client != "joao_silva" {
			t.Fatalf("ordem = %q/%q, want ana/joao_silva (ordem da chave)", debtors[0].Client, debtors[1].Client)
		}
		if debtors[0].Balance != 7000 || debtors[1].Balance != 3000 {
			t.Fatalf("saldos = %d/%d, want 7000/3000", debtors[0].Balance, debtors[1].Balance)
		}
	})
}

func TestStoresAgreeOnTheDayTimeline(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		record(t, s, mov(t, "m1", "João", "2026-08-01", 4000))
		record(t, s, mov(t, "m2", "Ana", "2026-08-02", 7000))
		record(t, s, mov(t, "m3", "João", "2026-08-02", -1000))

		page, err := s.DayMovements(context.Background(), user, day(t, "2026-08-02"), Page{})
		if err != nil {
			t.Fatalf("day movements: %v", err)
		}
		if len(page.Movements) != 2 {
			t.Fatalf("o dia tem %d movimentos, want 2", len(page.Movements))
		}
		// The day's own order is the key's: the client, then the ULID.
		if page.Movements[0].Client != "ana" || page.Movements[1].Client != "joao" {
			t.Fatalf("ordem do dia = %q/%q, want ana/joao", page.Movements[0].Client, page.Movements[1].Client)
		}
		if page.NextCursor != "" {
			t.Fatalf("next cursor = %q numa lista que acabou, want vazio", page.NextCursor)
		}
	})
}

func TestStoresAgreeOnClientPagination(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for i, date := range []string{"2026-08-01", "2026-08-02", "2026-08-03", "2026-08-04", "2026-08-05"} {
			record(t, s, mov(t, fmt.Sprintf("m%d", i), "João", date, 1000))
		}

		first, err := s.ClientMovements(ctx, user, "joao", Page{Limit: 2})
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		if len(first.Movements) != 2 {
			t.Fatalf("página 1 tem %d movimentos, want 2", len(first.Movements))
		}
		// Most recent first — that is the whole reason the client's copy is read
		// backwards.
		if got := first.Movements[0].Date.String(); got != "2026-08-05" {
			t.Fatalf("primeiro movimento = %s, want 2026-08-05 (mais recente primeiro)", got)
		}
		if first.NextCursor == "" {
			t.Fatalf("next cursor vazio com 3 movimentos restantes — uma lista cortada não pode sair calada")
		}

		var seen []string
		cursor := first.NextCursor
		for _, m := range first.Movements {
			seen = append(seen, m.Date.String())
		}
		for cursor != "" {
			page, err := s.ClientMovements(ctx, user, "joao", Page{Limit: 2, Cursor: cursor})
			if err != nil {
				t.Fatalf("página com cursor %q: %v", cursor, err)
			}
			for _, m := range page.Movements {
				seen = append(seen, m.Date.String())
			}
			cursor = page.NextCursor
		}
		want := []string{"2026-08-05", "2026-08-04", "2026-08-03", "2026-08-02", "2026-08-01"}
		if fmt.Sprint(seen) != fmt.Sprint(want) {
			t.Fatalf("paginação devolveu %v, want %v (sem repetir nem pular)", seen, want)
		}
	})
}

// A cursor from another timeline would page through the wrong rows and present
// them as this client's.
func TestStoresRejectAForeignCursor(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		record(t, s, mov(t, "m1", "João", "2026-08-01", 4000))
		record(t, s, mov(t, "m2", "Ana", "2026-08-01", 4000))

		_, err := s.ClientMovements(context.Background(), user, "joao", Page{
			Limit:  1,
			Cursor: clientSK("ana", day(t, "2026-08-01"), "m2"),
		})
		if err == nil {
			t.Fatal("cursor de outro cliente foi aceito")
		}
	})
}

func TestStoresRefuseMovementsThatCannotBeRecorded(t *testing.T) {
	cases := map[string]struct {
		build func(t *testing.T) Movement
		want  error
	}{
		"sem cliente": {
			build: func(t *testing.T) Movement {
				m := mov(t, "m1", "João", "2026-08-01", 4000)
				m.Client = ""
				return m
			},
			want: ErrNoClient,
		},
		"valor zero": {
			build: func(t *testing.T) Movement {
				m := mov(t, "m1", "João", "2026-08-01", 4000)
				m.Amount = 0
				return m
			},
			want: ErrZeroAmount,
		},
		"valor absurdo": {
			build: func(t *testing.T) Movement {
				m := mov(t, "m1", "João", "2026-08-01", 4000)
				m.Amount = MaxAmountCentavos + 1
				return m
			},
			want: ErrAmountTooLarge,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			eachStore(t, func(t *testing.T, s Store) {
				_, err := s.Record(context.Background(), tc.build(t))
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				// And nothing was written on the way to refusing.
				debtors, err := s.ListDebtors(context.Background(), user)
				if err != nil {
					t.Fatalf("list debtors: %v", err)
				}
				if len(debtors) != 0 {
					t.Fatalf("caderninho ficou com %d linhas após uma recusa", len(debtors))
				}
			})
		})
	}
}

func TestStoresKeepUsersApart(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		mine := mov(t, "m1", "João", "2026-08-01", 4000)
		theirs := mov(t, "m2", "João", "2026-08-01", 9900)
		theirs.UserID = "u2"
		record(t, s, mine)
		record(t, s, theirs)

		d, err := s.Debtor(ctx, user, "joao")
		if err != nil {
			t.Fatalf("debtor: %v", err)
		}
		if d.Balance != 4000 {
			t.Fatalf("saldo = %d, want 4000 — o caderninho de outro usuário vazou", d.Balance)
		}
		page, err := s.DayMovements(ctx, user, day(t, "2026-08-01"), Page{})
		if err != nil {
			t.Fatalf("day movements: %v", err)
		}
		if len(page.Movements) != 1 {
			t.Fatalf("o dia tem %d movimentos, want 1", len(page.Movements))
		}
	})
}
