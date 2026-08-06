// Package notifications derives financial alerts from a user's entries, monthly
// income and goal. It is the server-side twin of the dashboard's client-side
// useNotifications hook (apps/web/src/lib/notifications.ts) — kept as one pure
// function so the bell/history in the UI and the scheduled WhatsApp notifier
// can't drift apart.
package notifications

import (
	"fmt"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// MaxOverdue caps how many overdue bills are listed, so a large backlog can't
// produce an unbounded message (mirrors the web hook's cap).
const MaxOverdue = 3

// Kind labels an alert's source so callers can filter or style by type.
type Kind string

const (
	KindDueToday Kind = "due_today"
	KindOverdue  Kind = "overdue"
	KindGoal     Kind = "goal"
)

// Alert is one line in the feed.
type Alert struct {
	Kind Kind
	Text string // pt-BR, ready to render or send
}

// BillStatus is what the ledger's unpaid bills look like on one day, before any
// preference is applied to them.
//
// Evaluate answers a different question — "what is this user subscribed to
// hearing about" — and is deliberately silent about the kinds they turned off.
// A digest that goes out every day, quiet days included, needs this one too: to
// write "nada vence hoje" it has to know that nothing is due, not merely that
// nothing was reported. The two coincide only for a user with every alert
// enabled, and saying the first while meaning the second is the one sentence in
// the message that would be a lie.
type BillStatus struct {
	DueTodayCount int
	DueToday      int64
	OverdueCount  int
	OverdueTotal  int64
}

// Quiet reports a day with no bill asking for anything: nothing falling due and
// nothing already late. It is the only condition under which a digest may tell
// someone their bills are in order.
func (s BillStatus) Quiet() bool { return s.DueTodayCount == 0 && s.OverdueCount == 0 }

// Bills summarizes the unpaid expenses in `entries` as of `today`. Same window
// requirement as Evaluate: entries should cover at least the overdue look-back
// through today.
func Bills(entries []domain.FinancialEntry, today time.Time) BillStatus {
	dueToday, overdue := pendingBills(entries, today)

	status := BillStatus{DueTodayCount: len(dueToday), OverdueCount: len(overdue)}
	for _, e := range dueToday {
		status.DueToday += e.Amount
	}
	for _, e := range overdue {
		status.OverdueTotal += e.Amount
	}
	return status
}

// pendingBills splits the unpaid expenses into the ones falling due today and
// the ones whose date has already passed. Overdue comes back most recent first,
// matching the web hook's ordering.
//
// Both Evaluate and Bills read the same two lists, which is why the split lives
// here: an alert saying a bill is late and a digest saying none is must never be
// able to disagree about which bills those are.
func pendingBills(entries []domain.FinancialEntry, today time.Time) (dueToday, overdue []domain.FinancialEntry) {
	for _, e := range entries {
		if e.Type != domain.EntryTypeExpense || e.PaymentStatus != domain.PaymentStatusPending {
			continue
		}
		d := effectiveDate(e)
		switch {
		case sameDay(d, today):
			dueToday = append(dueToday, e)
		case d.Before(today):
			overdue = append(overdue, e)
		}
	}
	sortByEffectiveDateDesc(overdue)
	return dueToday, overdue
}

// effectiveDate mirrors packages/finance: a pending bill counts on its DueDate,
// falling back to the registration Date once settled.
func effectiveDate(e domain.FinancialEntry) time.Time {
	if e.DueDate != nil {
		return e.DueDate.Time()
	}
	return e.TransactionDate.Time()
}

// sameDay reports whether two times fall on the same calendar day (comparing
// only Y/M/D, ignoring any time-of-day component).
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// Evaluate returns the alerts that apply for `today`, filtered by `prefs` — a
// disabled alert type is skipped. `entries` should cover at least the overdue
// look-back window through today; `revenue` is the current month's faturamento
// (sales only — see domain.IsRevenue) and `goal` its target (a zero
// RevenueTarget disables the goal alert).
//
// The goal alert must fire on faturamento and never on entradas de caixa: a
// loan landing in the account is not a month's target being met, and telling
// someone otherwise is worse than saying nothing.
func Evaluate(
	prefs domain.NotificationPrefs,
	entries []domain.FinancialEntry,
	revenue int64,
	goal domain.Goal,
	today time.Time,
) []Alert {
	var alerts []Alert

	dueTodayBills, overdue := pendingBills(entries, today)

	if prefs.NotifyDueToday {
		var dueToday int64
		for _, e := range dueTodayBills {
			dueToday += e.Amount
		}
		if dueToday > 0 {
			alerts = append(alerts, Alert{
				Kind: KindDueToday,
				Text: fmt.Sprintf("Pagamento de R$ %s vence hoje", formatBRL(dueToday)),
			})
		}
	}

	if prefs.NotifyOverdue {
		for i, e := range overdue {
			if i >= MaxOverdue {
				break
			}
			desc := e.Description
			if desc == "" {
				desc = "Conta"
			}
			alerts = append(alerts, Alert{
				Kind: KindOverdue,
				Text: fmt.Sprintf("%s está vencida (venceu em %s)", desc, effectiveDate(e).Format("02/01")),
			})
		}
	}

	if prefs.NotifyGoal && goal.RevenueTarget > 0 && revenue >= goal.RevenueTarget {
		alerts = append(alerts, Alert{
			Kind: KindGoal,
			Text: "Meta de faturamento atingida!",
		})
	}

	return alerts
}

func sortByEffectiveDateDesc(entries []domain.FinancialEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && effectiveDate(entries[j]).After(effectiveDate(entries[j-1])); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// formatBRL renders centavos as Brazilian currency digits ("2850000" ->
// "28.500,00"), matching the webhook's money() helper.
func formatBRL(centavos int64) string {
	if centavos < 0 {
		centavos = -centavos
	}
	s := fmt.Sprintf("%d,%02d", centavos/100, centavos%100)
	// Insert thousands separators into the integer part.
	for i := len(s) - 6; i > 0; i -= 4 {
		s = s[:i] + "." + s[i:]
	}
	return s
}
