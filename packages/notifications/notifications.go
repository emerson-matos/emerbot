// Package notifications derives financial alerts from a user's entries, monthly
// income and goal. It is the server-side twin of the dashboard's client-side
// useNotifications hook (apps/web/src/lib/notifications.ts) — kept as one pure
// function so the bell/history in the UI and the scheduled WhatsApp notifier
// can't drift apart.
package notifications

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
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

// BillStatus is what the ledger's unpaid bills look like on one day, counted
// rather than listed.
//
// It is what lets the digest say "nada vence hoje" from the ledger itself
// instead of inferring it from an empty alert list. The two agree now that
// nothing filters the alerts, but they agree by construction of code that could
// drift apart again, and the claim is the kind that has to be true: a morning
// message telling a pharmacy its bills are in order is not a place to reason
// from an absence.
type BillStatus struct {
	DueTodayCount int
	DueToday      int64
	OverdueCount  int
	OverdueTotal  int64
}

// Quiet reports a day with no bill asking for anything: nothing falling due and
// nothing already late. It is the condition under which the digest may tell
// someone their bills are in order — see calmLine in apps/notifier.
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

// Bill is one unpaid expense written out for reading. Text is pt-BR and ready
// to send, like Alert.Text — Amount rides along so a caller can total or budget
// a list of them without parsing its own copy back.
type Bill struct {
	Amount int64
	Text   string
}

// DueToday lists the bills falling due today, one by one, biggest first.
//
// It exists beside the due-today Alert, which is a single total, because a
// total is a warning and a list is a task: "R$ 3.000,00 vencem hoje" tells
// someone to open the app, while the four lines behind it are what they
// actually have to pay. The two are delivered as separate WhatsApp messages —
// see apps/notifier — precisely so the list can be complete without pushing the
// digest past what a phone will show.
//
// Biggest first because that is the order the list gets read in when it is long
// and the reader is in a hurry, and because it is stable: entries added in the
// same minute have no meaningful ledger order to preserve.
func DueToday(entries []domain.FinancialEntry, today time.Time) []Bill {
	dueToday, _ := pendingBills(entries, today)
	sortByAmountDesc(dueToday)

	bills := make([]Bill, 0, len(dueToday))
	for _, e := range dueToday {
		bills = append(bills, Bill{Amount: e.Amount, Text: billText(e)})
	}
	return bills
}

// billText words one line of the list. The supplier is appended only when it
// adds something: it is a free-text field (see the epic behind ADR-023), so it
// is as often empty as it is a copy of the description.
func billText(e domain.FinancialEntry) string {
	desc := strings.TrimSpace(e.Description)
	if desc == "" {
		desc = "Conta"
	}
	line := fmt.Sprintf("R$ %s — %s", FormatBRL(e.Amount), desc)
	if s := strings.TrimSpace(e.Supplier); s != "" && !strings.EqualFold(s, desc) {
		line += " (" + s + ")"
	}
	return line
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

// Evaluate returns every alert that applies for `today`. `entries` should cover
// at least the overdue look-back window through today; `revenue` is the current
// month's faturamento (sales only — see domain.IsRevenue) and `goal` its target
// (a zero RevenueTarget is "no goal set", and yields no goal alert).
//
// It used to take the recipient's preferences and drop the kinds they had
// switched off. Nothing filters now: the alerts a pharmacy asked to be told
// about are all of them, and a rule that fires for one reader and not another
// meant the digest could truthfully say "nada vence hoje" to someone with four
// bills due — see the calm line in apps/notifier, which existed to work around
// exactly that.
//
// The goal alert must fire on faturamento and never on entradas de caixa: a
// loan landing in the account is not a month's target being met, and telling
// someone otherwise is worse than saying nothing.
func Evaluate(entries []domain.FinancialEntry, revenue int64, goal domain.Goal, today time.Time) []Alert {
	var alerts []Alert

	dueTodayBills, overdue := pendingBills(entries, today)

	var dueToday int64
	for _, e := range dueTodayBills {
		dueToday += e.Amount
	}
	if dueToday > 0 {
		alerts = append(alerts, Alert{
			Kind: KindDueToday,
			Text: fmt.Sprintf("Pagamento de R$ %s vence hoje", FormatBRL(dueToday)),
		})
	}

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

	if goal.RevenueTarget > 0 && revenue >= goal.RevenueTarget {
		alerts = append(alerts, Alert{
			Kind: KindGoal,
			Text: "Meta de faturamento atingida!",
		})
	}

	return alerts
}

// sortByAmountDesc puts the biggest bill first; sortByEffectiveDateDesc the most
// recent. Both are stable, so bills that tie keep the order the ledger returned
// them in — which is the only order left to preserve once the key is equal.
func sortByAmountDesc(entries []domain.FinancialEntry) {
	slices.SortStableFunc(entries, func(a, b domain.FinancialEntry) int {
		return cmp.Compare(b.Amount, a.Amount)
	})
}

func sortByEffectiveDateDesc(entries []domain.FinancialEntry) {
	slices.SortStableFunc(entries, func(a, b domain.FinancialEntry) int {
		return effectiveDate(b).Compare(effectiveDate(a))
	})
}

// FormatBRL renders centavos as Brazilian currency digits ("2850000" ->
// "28.500,00"), matching the webhook's money() helper. It is exported so the
// notifier can total the day's bill list in the same wording the alert beside it
// uses — the total and the lines under it disagreeing about a thousands
// separator is the kind of detail that makes a reader distrust both.
func FormatBRL(centavos int64) string {
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
