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

// Dead bands, in percentage points: movement inside one is noise, not a
// direction the user is expected to act on. A month and a week get different
// ones because a week is the shorter, noisier window.
const (
	monthDeadBandPct = 2
	weekDeadBandPct  = 5
)

// buildTrend expresses current as a percentage change from the previous month.
func buildTrend(current, previous int64) MonthTrend {
	return trendOver(current, previous, monthDeadBandPct)
}

// trendOver is the one percentage-change reading in this package, and every
// consumer that decides "subiu" or "caiu" reads what it returns rather than
// dividing again.
//
// It used to have a twin. `percentChange` computed the same thing over the same
// inputs with a raw denominator and no dead band, and the insights ran on it
// while the recommendations ran on this — so a month at -10.4% got the "Faturamento
// caiu" warning and no recommendation to go with it, the two having rounded the
// same number differently. The browser then computed it a third time to caption
// the projection card, without the dead band, so a +1% month printed "estável"
// in the KPI row and "↑ 1%" three sections below.
//
// A previous of zero has no meaningful percentage — anything over nothing is
// infinite growth — so it is reported as a flat 100% up (or stable at zero)
// rather than a division by zero. Consumers tell that case apart through
// Previous and say "sem base" instead of quoting the 100%. The denominator is
// the absolute value so a swing out of a negative balance reads as "up", not
// "down".
//
// Change is rounded before Direction is decided, so the direction a consumer
// acts on is the one the printed percentage supports: a figure shown as "↓ 10%"
// can never sit beside an insight that only fires above 10.
func trendOver(current, previous int64, deadBandPct int) MonthTrend {
	if previous == 0 {
		t := MonthTrend{Current: current, Previous: previous, Change: 0, Direction: TrendStable}
		if current > 0 {
			t.Change, t.Direction = 100, TrendUp
		}
		return t
	}

	change := (float64(current-previous) / math.Abs(float64(previous))) * 100
	rounded := roundToInt(change)

	direction := TrendStable
	switch {
	case rounded > deadBandPct:
		direction = TrendUp
	case rounded < -deadBandPct:
		direction = TrendDown
	}

	return MonthTrend{Current: current, Previous: previous, Change: rounded, Direction: direction}
}
