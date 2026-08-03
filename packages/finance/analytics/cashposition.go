package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"

	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// buildCashPosition summarizes where the balance stands today and where it is
// headed: the month-end figure, the worst point, and how long before it crosses
// zero.
//
// The points it is given are the *booked* curve — everything already recorded,
// nothing else. That curve is honest about the past and systematically wrong
// about the future, because the two sides of a pharmacy's month are recorded at
// opposite times: rent, payroll and the suppliers are all booked on the 1st for
// the whole month, while a sale is only recorded when it happens. So the curve
// after today is expenses with no counterpart, and it dives.
//
// That is where "Saldo fica negativo em 1 dia" came from, at the start of every
// month, on a pharmacy whose balance was never in danger. It is the same fault
// ADR-017 found in the health verdict — judging a month by bills that are merely
// scheduled — surviving in the one reading it did not reach.
//
// So the days from today on are credited with what an ordinary day of that
// weekday brings in, `rates` (see cashInRates), net of whatever is already
// booked for it. Expenses are left exactly as booked: they genuinely are known
// in advance here, and guessing at unbooked ones would soften an alarm on
// invented evidence. The projection therefore leans pessimistic, which is the
// right direction for a warning to lean.
//
// CashFlowPoint.Date is a plain calendar day, so "today" is now's calendar day
// in whatever location the caller anchored it to — deriving it from an instant
// in UTC would roll over to tomorrow during the evening in Brazil and lose the
// current balance entirely.
func buildCashPosition(points []pkgfinance.CashFlowPoint, rates dailyRates, now time.Time) CashPosition {
	today := now.Format("2006-01-02")

	if len(points) == 0 {
		return CashPosition{LowestProjectedDate: today}
	}

	var currentBalance int64
	for _, p := range points {
		if p.Date == today {
			currentBalance = p.RunningBalance
			break
		}
	}

	position := CashPosition{
		// Today's balance is booked fact and stays that way: it is money in the
		// account, not a forecast, and inflating it with an average would make
		// the one figure on this card that can be checked against a bank app
		// the one that disagrees with it.
		CurrentBalance:      currentBalance,
		LowestProjected:     currentBalance,
		LowestProjectedDate: today,
		ExpectsReceipts:     rates.sample > 0,
	}

	// expected accumulates the receipts the days from today on are expected to
	// bring but have not booked, so each point's projected balance is its booked
	// one plus everything expected up to and including that day.
	var expected int64
	for _, p := range points {
		if p.Date >= today {
			expected += expectedExtraIncome(p, rates)
		}
		balance := p.RunningBalance + expected

		if balance < position.LowestProjected {
			position.LowestProjected = balance
			position.LowestProjectedDate = p.Date
		}
		if balance < 0 && position.DaysUntilNegative == nil && p.Date > today {
			// Calendar days apart, not elapsed hours — otherwise the answer
			// would depend on the time of day the analysis happens to run.
			days := calendarDaysBetween(today, p.Date)
			position.DaysUntilNegative = &days
		}
		position.EndOfMonthProjection = balance
	}

	return position
}

// expectedExtraIncome is what a day is still expected to receive beyond what it
// has already booked. A day that has booked more than its weekday usually brings
// adds nothing rather than a negative: the rate is an ordinary day's takings,
// not a ceiling on a good one.
//
// Netting off the booked amount is what keeps a day from being counted twice —
// today's takings so far are already in the running balance, and a crediário
// instalment falling due next Friday is both booked for that day and part of
// what an average Friday receives.
func expectedExtraIncome(p pkgfinance.CashFlowPoint, rates dailyRates) int64 {
	date, err := domain.ParseDay(p.Date)
	if err != nil {
		return 0
	}
	return max(0, rates.avg[int(date.Weekday())]-p.ProjectedIncome)
}

// calendarDaysBetween returns the number of whole calendar days from `from` to
// `to`, both "YYYY-MM-DD". Unparseable input yields 0 rather than a panic; the
// dates come from the store, so that only happens if the store is broken.
func calendarDaysBetween(from, to string) int {
	start, err := domain.ParseDay(from)
	if err != nil {
		return 0
	}
	end, err := domain.ParseDay(to)
	if err != nil {
		return 0
	}
	return int(end.Sub(start).Hours() / 24)
}
