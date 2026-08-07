package notifications

import (
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptrCD(t time.Time) *domain.CalendarDate { cd := domain.NewCalendarDate(t); return &cd }

func expense(desc string, amount int64, due string, status domain.PaymentStatus) domain.FinancialEntry {
	return domain.FinancialEntry{
		EntryID:       domain.EntryID(desc),
		Description:   desc,
		Amount:        amount,
		Type:          domain.EntryTypeExpense,
		PaymentStatus: status,
		DueDate:       ptrCD(day(due)),
	}
}

func kinds(alerts []Alert) []Kind {
	out := make([]Kind, len(alerts))
	for i, a := range alerts {
		out[i] = a.Kind
	}
	return out
}

func TestEvaluateDueTodayAggregatesPendingExpenses(t *testing.T) {
	today := day("2026-07-20")
	entries := []domain.FinancialEntry{
		expense("Fornecedor", 285000, "2026-07-20", domain.PaymentStatusPending),
		expense("Luz", 15000, "2026-07-20", domain.PaymentStatusPending),
		// paid today — must not count
		expense("Ja pago", 99900, "2026-07-20", domain.PaymentStatusPaid),
	}

	alerts := Evaluate(entries, 0, domain.Goal{}, today)

	if len(alerts) != 1 || alerts[0].Kind != KindDueToday {
		t.Fatalf("want one due_today alert, got %+v", alerts)
	}
	if want := "Pagamento de R$ 3.000,00 vence hoje"; alerts[0].Text != want {
		t.Fatalf("text = %q, want %q", alerts[0].Text, want)
	}
}

func TestEvaluateOverdueSortedAndCapped(t *testing.T) {
	today := day("2026-07-20")
	entries := []domain.FinancialEntry{
		expense("Mais antiga", 1000, "2026-07-01", domain.PaymentStatusPending),
		expense("Recente", 2000, "2026-07-18", domain.PaymentStatusPending),
		expense("Meio", 3000, "2026-07-10", domain.PaymentStatusPending),
		expense("Antiga2", 4000, "2026-07-05", domain.PaymentStatusPending),
	}

	alerts := Evaluate(entries, 0, domain.Goal{}, today)

	if len(alerts) != MaxOverdue {
		t.Fatalf("want %d overdue alerts (capped), got %d", MaxOverdue, len(alerts))
	}
	// Most recent overdue first.
	if alerts[0].Text != "Recente está vencida (venceu em 18/07)" {
		t.Fatalf("first overdue = %q", alerts[0].Text)
	}
}

func TestEvaluateOrderDueTodayThenOverdueThenGoal(t *testing.T) {
	today := day("2026-07-20")
	entries := []domain.FinancialEntry{
		expense("Hoje", 10000, "2026-07-20", domain.PaymentStatusPending),
		expense("Vencida", 20000, "2026-07-01", domain.PaymentStatusPending),
	}
	goal := domain.Goal{RevenueTarget: 100}

	got := kinds(Evaluate(entries, 100, goal, today))
	want := []Kind{KindDueToday, KindOverdue, KindGoal}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
}

// TestBillsCountsWhatTheDayOwes: BillStatus is the ledger's own count, which is
// what the digest's "nada vence hoje" is built on. It is deliberately not read
// off the alert list — the two say the same thing today only because nothing
// filters the alerts, and that is a property of Evaluate, not a guarantee.
func TestBillsCountsWhatTheDayOwes(t *testing.T) {
	today := day("2026-07-20")
	entries := []domain.FinancialEntry{
		expense("Fornecedor", 285000, "2026-07-20", domain.PaymentStatusPending),
		expense("Luz", 15000, "2026-07-20", domain.PaymentStatusPending),
		expense("Ja pago", 99900, "2026-07-20", domain.PaymentStatusPaid),
		expense("Vencida", 20000, "2026-07-01", domain.PaymentStatusPending),
		expense("Vencida quitada", 50000, "2026-07-02", domain.PaymentStatusPaid),
		// Still ahead: neither due today nor late.
		expense("Aluguel", 300000, "2026-07-25", domain.PaymentStatusPending),
	}

	got := Bills(entries, today)
	// What is already paid counts nowhere: it is not what anyone has left to do.
	want := BillStatus{DueTodayCount: 2, DueToday: 300000, OverdueCount: 1, OverdueTotal: 20000}
	if got != want {
		t.Fatalf("Bills = %+v, want %+v", got, want)
	}
	if got.Quiet() {
		t.Error("Quiet() on a day with two bills due and one late")
	}
}

// TestEvaluateGoalNeedsATargetAndTheRevenueToClearIt keeps the goal rule itself
// under test now that the toggle that used to gate it is gone: a month with no
// target set is not a month that met one.
func TestEvaluateGoalNeedsATargetAndTheRevenueToClearIt(t *testing.T) {
	today := day("2026-07-20")
	goal := domain.Goal{RevenueTarget: 5000000}

	if a := Evaluate(nil, 4999999, goal, today); len(a) != 0 {
		t.Fatalf("below target should yield no alert, got %+v", a)
	}
	a := Evaluate(nil, 5000000, goal, today)
	if len(a) != 1 || a[0].Kind != KindGoal {
		t.Fatalf("want goal alert at target, got %+v", a)
	}
	// No goal set at all: a target of zero is not a target that was met.
	if a := Evaluate(nil, 6000000, domain.Goal{}, today); len(a) != 0 {
		t.Fatalf("no target means no goal alert, got %+v", a)
	}
}

func TestBillsQuietDay(t *testing.T) {
	today := day("2026-07-20")
	entries := []domain.FinancialEntry{
		expense("Ja pago", 99900, "2026-07-20", domain.PaymentStatusPaid),
		expense("Aluguel", 300000, "2026-07-25", domain.PaymentStatusPending),
		// An income entry that is still to be received is not a bill.
		{EntryID: "venda", Amount: 100000, Type: domain.EntryTypeIncome, PaymentStatus: domain.PaymentStatusPending, DueDate: ptrCD(day("2026-07-19"))},
	}

	if got := Bills(entries, today); got != (BillStatus{}) {
		t.Fatalf("Bills = %+v, want an empty status: neither entry is an unpaid bill of today's", got)
	}
}

// TestDueTodayWordsAndOrdersTheList covers the copy directly rather than through
// the notifier, where a regression here reports as a WhatsApp-message failure
// three packages away.
func TestDueTodayWordsAndOrdersTheList(t *testing.T) {
	today := day("2026-07-20")
	withSupplier := func(desc, supplier string, amount int64) domain.FinancialEntry {
		e := expense(desc, amount, "2026-07-20", domain.PaymentStatusPending)
		e.Supplier = supplier
		return e
	}
	entries := []domain.FinancialEntry{
		withSupplier("Energia", "", 13500),
		withSupplier("Distribuidora", "Santa Cruz LTDA", 285000),
		// A supplier that only repeats the description adds nothing to the line.
		withSupplier("Folha de pagamento", "folha de PAGAMENTO", 900000),
		// No description at all still has to name something payable.
		withSupplier("", "", 4200),
		// Not today's, and not unpaid: neither belongs on the list.
		expense("Vencida", 20000, "2026-07-01", domain.PaymentStatusPending),
		expense("Ja pago", 99900, "2026-07-20", domain.PaymentStatusPaid),
	}

	got := DueToday(entries, today)

	want := []Bill{
		{Amount: 900000, Text: "R$ 9.000,00 — Folha de pagamento"},
		{Amount: 285000, Text: "R$ 2.850,00 — Distribuidora (Santa Cruz LTDA)"},
		{Amount: 13500, Text: "R$ 135,00 — Energia"},
		{Amount: 4200, Text: "R$ 42,00 — Conta"},
	}
	if len(got) != len(want) {
		t.Fatalf("DueToday returned %d bills, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bill %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestDueTodayKeepsLedgerOrderOnTies: the sort is by amount, and equal amounts
// have no second key worth inventing — so they must come back in the order they
// arrived, not in whatever order an unstable sort leaves them.
func TestDueTodayKeepsLedgerOrderOnTies(t *testing.T) {
	today := day("2026-07-20")
	entries := []domain.FinancialEntry{
		expense("Primeira", 50000, "2026-07-20", domain.PaymentStatusPending),
		expense("Segunda", 50000, "2026-07-20", domain.PaymentStatusPending),
		expense("Terceira", 50000, "2026-07-20", domain.PaymentStatusPending),
	}

	got := DueToday(entries, today)

	for i, want := range []string{"Primeira", "Segunda", "Terceira"} {
		if !strings.Contains(got[i].Text, want) {
			t.Errorf("bill %d = %q, want %q — ties must keep ledger order", i, got[i].Text, want)
		}
	}
}
