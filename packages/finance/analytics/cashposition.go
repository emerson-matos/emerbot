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
	// The forecast covers one calendar month, so on the last day of it — and on
	// any day of a month already closed — no point matches and NextDay stays
	// nil. That is the honest answer: the next day's balance is the next
	// month's business, and this forecast has never seen it.
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")

	if len(points) == 0 {
		// No curve at all: nothing to grade the bills against, and "coberto"
		// over an empty forecast would be a verdict on no evidence.
		return CashPosition{LowestProjectedDate: today, Commitments: CommitmentsUnknown}
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
		ExpectsReceipts:     rates.sample() > 0,
		Forecast:            make([]DayCash, 0, len(points)),
	}

	// expected accumulates the receipts the days from today on are expected to
	// bring but have not booked, so each point's projected balance is its booked
	// one plus everything expected up to and including that day.
	//
	// nextDay is an index rather than a pointer taken inside the loop: append
	// may move the backing array, and a pointer into it would then address the
	// old one. It is resolved once the series has stopped growing.
	var expected int64
	nextDay := -1
	for _, p := range points {
		var extra int64
		if p.Date >= today {
			extra = expectedExtraIncome(p, rates)
			expected += extra
		}
		balance := p.RunningBalance + expected

		// The curve, kept rather than reduced to its extremes on the way past.
		// The days before today add nothing to their booked balance — expected is
		// still zero there — so the same series carries the realised half and the
		// projected one, and a consumer splits it at today rather than fetching a
		// second curve to draw the left of the chart with.
		position.Forecast = append(position.Forecast, DayCash{
			Date:         p.Date,
			Balance:      balance,
			ScheduledIn:  p.ProjectedIncome,
			ScheduledOut: p.ProjectedExpense,
			ExpectedIn:   extra,
		})
		if p.Date == tomorrow {
			nextDay = len(position.Forecast) - 1
		}

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
	if nextDay >= 0 {
		position.NextDay = &position.Forecast[nextDay]
	}
	position.Commitments = commitmentCoverage(position)

	return position
}

// commitmentCoverage grades the month's booked bills against the balance that
// is coming, so a consumer has a verdict instead of a total to editorialise on.
//
// It reads the trough rather than DaysUntilNegative: that field only counts
// crossings still ahead, so a balance already under water today would otherwise
// be graded "coberto" — the one reading where saying so is worst.
func commitmentCoverage(position CashPosition) CommitmentCoverage {
	switch {
	case !position.ExpectsReceipts:
		return CommitmentsUnknown
	case position.LowestProjected < 0:
		return CommitmentsUncovered
	default:
		return CommitmentsCovered
	}
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
