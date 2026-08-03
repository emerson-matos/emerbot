package analytics

import "math"

// buildTrends expresses the analysed month against the previous one, both
// measured over the same finished days — see buildComparison.
//
// A month with no comparable window yet gets flat, empty trends rather than a
// direction: there is nothing behind us that can be held against last month on
// the same terms. That covers the month's first day, where nothing has finished
// at all, and its first week, where what has finished is a different set of
// weekdays from the one the previous month offers. Consumers tell those apart
// through Analysis.Period — ThroughDay 0 for the former,
// ComparableThroughDay 0 for both.
func buildTrends(c comparison) Trends {
	if !c.comparable() {
		return Trends{
			Faturamento: MonthTrend{Direction: TrendStable},
			Despesa:     MonthTrend{Direction: TrendStable},
			Resultado:   MonthTrend{Direction: TrendStable},
		}
	}
	return Trends{
		// Faturamento is the performance trend, so it moves with sales only —
		// a loan must never read as the business growing. Resultado stays on
		// the broad income figure, because a result is money in minus money
		// out whatever the money was.
		Faturamento: buildTrend(c.current.revenue, c.previous.revenue),
		Despesa:     buildTrend(c.current.expense, c.previous.expense),
		Resultado:   buildTrend(c.current.balance, c.previous.balance),
	}
}

// buildTrend expresses current as a percentage change from previous.
//
// A previous of zero has no meaningful percentage — anything over nothing is
// infinite growth — so it is reported as a flat 100% up (or stable at zero)
// rather than a division by zero. The denominator is the absolute value so a
// swing out of a negative balance reads as "up", not "down".
func buildTrend(current, previous int64) MonthTrend {
	if previous == 0 {
		t := MonthTrend{Current: current, Previous: previous, Change: 0, Direction: TrendStable}
		if current > 0 {
			t.Change, t.Direction = 100, TrendUp
		}
		return t
	}

	change := (float64(current-previous) / math.Abs(float64(previous))) * 100
	rounded := roundToInt(change)

	// ±2% is a dead band: month-to-month noise should not be reported as a
	// direction the user is expected to act on.
	direction := TrendStable
	switch {
	case rounded > 2:
		direction = TrendUp
	case rounded < -2:
		direction = TrendDown
	}

	return MonthTrend{Current: current, Previous: previous, Change: rounded, Direction: direction}
}
