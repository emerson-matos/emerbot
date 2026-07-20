package notifications

import (
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

func ptr(t time.Time) *time.Time { return &t }

func expense(desc string, amount int64, due string, status domain.PaymentStatus) domain.FinancialEntry {
	return domain.FinancialEntry{
		EntryID:       desc,
		Description:   desc,
		Amount:        amount,
		Type:          domain.EntryTypeExpense,
		PaymentStatus: status,
		DueDate:       ptr(day(due)),
	}
}

func allOn() domain.NotificationPrefs {
	return domain.NotificationPrefs{
		UserID: "u", WAEnabled: true,
		NotifyDueToday: true, NotifyOverdue: true, NotifyGoal: true,
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

	alerts := Evaluate(allOn(), entries, 0, domain.Goal{}, today)

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

	alerts := Evaluate(allOn(), entries, 0, domain.Goal{}, today)

	if len(alerts) != MaxOverdue {
		t.Fatalf("want %d overdue alerts (capped), got %d", MaxOverdue, len(alerts))
	}
	// Most recent overdue first.
	if alerts[0].Text != "Recente está vencida (venceu em 18/07)" {
		t.Fatalf("first overdue = %q", alerts[0].Text)
	}
}

func TestEvaluateGoalReachedRespectsTargetAndPref(t *testing.T) {
	today := day("2026-07-20")
	goal := domain.Goal{RevenueTarget: 5000000}

	// Income below target -> no alert.
	if a := Evaluate(allOn(), nil, 4999999, goal, today); len(a) != 0 {
		t.Fatalf("below target should yield no alert, got %+v", a)
	}
	// Income at target -> alert.
	a := Evaluate(allOn(), nil, 5000000, goal, today)
	if len(a) != 1 || a[0].Kind != KindGoal {
		t.Fatalf("want goal alert, got %+v", a)
	}
	// Pref off -> no alert even when reached.
	prefs := allOn()
	prefs.NotifyGoal = false
	if a := Evaluate(prefs, nil, 6000000, goal, today); len(a) != 0 {
		t.Fatalf("goal pref off should suppress alert, got %+v", a)
	}
}

func TestEvaluateDisabledTypesAreSkipped(t *testing.T) {
	today := day("2026-07-20")
	entries := []domain.FinancialEntry{
		expense("Hoje", 10000, "2026-07-20", domain.PaymentStatusPending),
		expense("Vencida", 20000, "2026-07-01", domain.PaymentStatusPending),
	}
	prefs := domain.NotificationPrefs{UserID: "u", NotifyDueToday: false, NotifyOverdue: false, NotifyGoal: false}

	if a := Evaluate(prefs, entries, 0, domain.Goal{}, today); len(a) != 0 {
		t.Fatalf("all types off should yield no alerts, got %+v", a)
	}
}

func TestEvaluateOrderDueTodayThenOverdueThenGoal(t *testing.T) {
	today := day("2026-07-20")
	entries := []domain.FinancialEntry{
		expense("Hoje", 10000, "2026-07-20", domain.PaymentStatusPending),
		expense("Vencida", 20000, "2026-07-01", domain.PaymentStatusPending),
	}
	goal := domain.Goal{RevenueTarget: 100}

	got := kinds(Evaluate(allOn(), entries, 100, goal, today))
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
