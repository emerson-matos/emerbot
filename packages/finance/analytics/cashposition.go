package analytics

import (
	"time"

	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// buildCashPosition summarizes the month's daily balance projection: where the
// balance stands today, where it lands at month end, its worst point, and how
// long before it crosses zero.
//
// CashFlowPoint.Date is a plain calendar day, so "today" is now's calendar day
// in whatever location the caller anchored it to — deriving it from an instant
// in UTC would roll over to tomorrow during the evening in Brazil and lose the
// current balance entirely.
func buildCashPosition(points []pkgfinance.CashFlowPoint, now time.Time) CashPosition {
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
		CurrentBalance:       currentBalance,
		EndOfMonthProjection: points[len(points)-1].RunningBalance,
		LowestProjected:      currentBalance,
		LowestProjectedDate:  today,
	}

	for _, p := range points {
		if p.RunningBalance < position.LowestProjected {
			position.LowestProjected = p.RunningBalance
			position.LowestProjectedDate = p.Date
		}
		if p.RunningBalance < 0 && position.DaysUntilNegative == nil && p.Date > today {
			// Calendar days apart, not elapsed hours — otherwise the answer
			// would depend on the time of day the analysis happens to run.
			days := calendarDaysBetween(today, p.Date)
			position.DaysUntilNegative = &days
		}
	}

	return position
}

// calendarDaysBetween returns the number of whole calendar days from `from` to
// `to`, both "YYYY-MM-DD". Unparseable input yields 0 rather than a panic; the
// dates come from the store, so that only happens if the store is broken.
func calendarDaysBetween(from, to string) int {
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return 0
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return 0
	}
	return int(end.Sub(start).Hours() / 24)
}
