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
// look-back window through today; `income` is the current month's total income
// and `goal` its target (a zero RevenueTarget disables the goal alert).
func Evaluate(
	prefs domain.NotificationPrefs,
	entries []domain.FinancialEntry,
	income int64,
	goal domain.Goal,
	today time.Time,
) []Alert {
	var alerts []Alert

	pending := make([]domain.FinancialEntry, 0, len(entries))
	for _, e := range entries {
		if e.Type == domain.EntryTypeExpense && e.PaymentStatus == domain.PaymentStatusPending {
			pending = append(pending, e)
		}
	}

	if prefs.NotifyDueToday {
		var dueToday int64
		for _, e := range pending {
			if sameDay(effectiveDate(e), today) {
				dueToday += e.Amount
			}
		}
		if dueToday > 0 {
			alerts = append(alerts, Alert{
				Kind: KindDueToday,
				Text: fmt.Sprintf("Pagamento de R$ %s vence hoje", formatBRL(dueToday)),
			})
		}
	}

	if prefs.NotifyOverdue {
		overdue := make([]domain.FinancialEntry, 0)
		for _, e := range pending {
			d := effectiveDate(e)
			if d.Before(today) && !sameDay(d, today) {
				overdue = append(overdue, e)
			}
		}
		// Most recent first, matching the web hook's ordering.
		sortByEffectiveDateDesc(overdue)
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

	if prefs.NotifyGoal && goal.RevenueTarget > 0 && income >= goal.RevenueTarget {
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
