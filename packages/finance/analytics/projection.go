package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// buildProjection projects where the month's faturamento lands if the days
// left trade like the same weekdays already have, and prices the gap to the
// income goal.
//
// This used to be computed twice, differently. The dashboard card derived a
// projection in the browser from the weekday averages, while ToolPayload
// reported "projecao_do_mes" from last week's flat daily rate — the same month,
// two numbers, depending on whether you asked the page or the bot. Worse, the
// card priced its own "necessário por dia" off its own projection while the
// health insight and the recommendation right above it priced theirs off what
// had actually come in, so the page told the user two different daily targets
// at the same time.
//
// It is computed once here, and the per-day ask that used to sit beside it is
// gone entirely: dividing what is missing by the days left asks the same of a
// Sunday and a Monday, which on a pharmacy whose Sundays bring a third of a
// Monday is a number nobody can hit. DayTarget replaces it — a day's share of
// the gap, at that day's own weekday rhythm. See ADR-019.
//
// todayRevenue is what has already been sold today; it is subtracted from
// today's own weekday average so the day is not counted twice — once inside
// Actual, once again as a day still expected to bring a full day's takings.
//
// rates price the days still to come, and they come from the trailing weeks
// rather than from the analysed month — see projectionRates for why a month's
// own first days cannot price themselves.
func buildProjection(rates dailyRates, goals GoalProgress, clock monthClock, todayRevenue int64) Projection {
	// A closed month says so before the rates are consulted at all. Its window is
	// full of trading, so asking them would report "janela" over a Projected that
	// is the month's realised faturamento and nothing else — and it lets callers
	// skip fetching a window they cannot use, without that turning into a
	// "sem_base" label.
	basis := ProjectionClosed
	if clock.inProgress {
		basis = rates.basis()
	}

	projection := Projection{
		Actual:        goals.RevenueActual,
		Projected:     goals.RevenueActual,
		Target:        goals.RevenueTarget,
		DaysRemaining: goals.DaysRemaining,
		Basis:         basis,
	}

	// From today, inclusive: today is a day the pharmacy can still sell on.
	// Starting at tomorrow wrote today off before it had happened, and on the
	// last day of the month left nothing at all to project.
	for day := clock.today; clock.inProgress && day <= clock.total; day++ {
		avg := rates.avg[int(clock.weekdayOf(day))]
		if day == clock.today {
			// Only the part of an ordinary day that has not happened yet is
			// still ahead of us.
			avg = max(0, avg-todayRevenue)
		}
		projection.Remaining += avg
	}
	projection.Projected += projection.Remaining

	// Today's ask, assigned exactly once and before the early return below, so
	// that a month with no goal still says *why* it has no ask instead of
	// carrying the zero value of a struct nobody filled in.
	//
	// The plan it comes from prices every remaining day, not just this one; the
	// rest are reachable through the tool rather than shipped as fields.
	projection.Plan = newPlan(rates, projection, basis, clock, todayRevenue)
	projection.TodayTarget = projection.Plan.at(rates, clock, clock.today, todayRevenue)
	projection.PlannedClose = projection.plannedClose(rates, clock, todayRevenue)

	if projection.Target <= 0 {
		return projection
	}

	// The verdict stands on a closed month too — it either reached its target
	// or it did not.
	projection.OnTrack = projection.Projected >= projection.Target
	projection.Coverage = float64(projection.Projected) / float64(projection.Target)
	projection.Status = projectionStatus(projection.Coverage)
	if gap := projection.Target - projection.Projected; gap > 0 {
		projection.Gap = gap
	}
	return projection
}

// newPlan distributes the gap over the days the month has left, and never asks
// a day for less than that day of the week usually brings. The guards run in
// the order a person would ask them: a month that has ended has no day left to
// sell on, a month with no goal has nothing to take a share of, and only then
// does the history behind the days matter.
//
// The floor is the whole point of the second half. Distributing the gap and
// nothing else meant that a month running ahead asked each day for *less* than
// it habitually sells — a Wednesday worth R$ 1.000,00 asked for R$ 820,00 —
// and a month whose goal was already met asked for nothing at all. Both are
// read as permission to trade below the pharmacy's own rhythm, and the second
// one is worse than the first: at 99% of the pace the day was asked for almost
// its average, at 101% it was asked for nothing. A target is a floor under the
// day, not a ceiling over it. See ADR-025.
//
// So the goal-met case is no longer an absence: it is a plan at exactly the
// rhythm, and the good news travels as Plan.GapFactor (below 1, or 0 when there
// is no gap left at all) rather than as a missing number. What the day is asked
// for is the greater of the two, which is what makes the plan close the month
// *and* keep the average.
//
// There is exactly one plan per analysis, and it is what makes today's ask and
// any other day's slices of one distribution rather than rival forecasts — see
// the Plan type for the invariant that follows from it.
func newPlan(rates dailyRates, projection Projection, basis ProjectionBasis, clock monthClock, todayRevenue int64) Plan {
	if !clock.inProgress {
		return Plan{State: DayTargetClosedMonth}
	}
	if projection.Target <= 0 {
		return Plan{State: DayTargetNoGoal}
	}

	var totalHistAvg int64
	for day := clock.today; day <= clock.total; day++ {
		totalHistAvg += rates.avg[int(clock.weekdayOf(day))]
	}
	// Asked before the gap is priced, and not after as it used to be: the floor
	// *is* the weekday average, so a window with nothing in it has no ask to
	// make either way. A goal already met used to short-circuit above this
	// check and reported "meta_batida" over a ledger that had never traded.
	if basis == ProjectionNoBasis || totalHistAvg <= 0 {
		return Plan{State: DayTargetNoHistory}
	}

	// Measured from what the till held when the day opened: totalHistAvg prices
	// today at a whole day's average, so a numerator net of the morning shrank
	// the target hour by hour against a whole-day Histórico.
	//
	// Clamped at zero because a goal already met is a gap of nothing, not a
	// negative one — a negative factor would ask the remaining days for less
	// than nothing.
	missing := max(0, projection.Target-(projection.Actual-todayRevenue))
	gap := float64(missing) / float64(totalHistAvg)

	return Plan{State: DayTargetOK, GapFactor: gap, Factor: max(1, gap)}
}

// plannedClose is where the month lands if every day still to come is sold at
// the ask the plan makes of it — as opposed to Projected, which is where it
// lands if they merely trade as usual.
//
// The two are different questions and only one of them was being answered. A
// pharmacy running ahead of its goal had a projection built on its averages and
// a plan that asked for less than those averages, so "se eu bater a meta todo
// dia, fecho em quanto?" resolved to the goal itself — below what the pharmacy
// would close at by doing nothing differently. With the floor in place the plan
// can only close the month at or above both, and this is that figure:
//
//	PlannedClose == max(Projected, meta do mês)
//
// It sums the days' own rounded asks rather than multiplying the total average
// by the factor, so it is exactly what get_meta_do_dia would add up to — the
// month's close and its days cannot disagree by a rounding.
//
// The max against Projected covers the one day that is not priced by the plan:
// today, when it has already sold more than its weekday usually brings. Projected
// counts what the till actually holds there; the plan still asks for a whole
// ordinary day, and a projection must never fall below takings already banked.
func (p Projection) plannedClose(rates dailyRates, clock monthClock, todayRevenue int64) int64 {
	if p.Plan.State != DayTargetOK {
		return p.Projected
	}
	var asked int64
	for day := clock.today; day <= clock.total; day++ {
		asked += roundToInt64(float64(rates.avg[int(clock.weekdayOf(day))]) * p.Plan.Factor)
	}
	// Measured from the morning, like every whole-day ask here: what today has
	// already sold is inside Actual and inside its own target.
	return max(p.Projected, p.Actual-todayRevenue+asked)
}

// at prices one day of the analysed month against the plan: that weekday's own
// average, scaled by the shared factor. realized is what the day has already
// sold, which is the only thing a closed day has to say and the missing half of
// what an open one does.
//
// day is a day of the month and may run past its last — that is how a caller
// asks about tomorrow on the 31st, or about a date it has not checked against
// the calendar. The answer is that the day belongs to a month this analysis
// knows nothing about, named rather than left as a silence: "there is no ask"
// and "the ask is for a month I cannot see from here" are different answers and
// only one of them is about this pharmacy.
//
// A day that has already closed gets no target at all, only its result and what
// that weekday usually brings. Pricing it against today's plan would be a target
// invented backwards — the plan distributes what is missing *now*, and the day
// it would be charging is one nobody can sell on any more. See ADR-021.
func (p Plan) at(rates dailyRates, clock monthClock, day int, realized int64) DayTarget {
	// Day 0 is what a closed month has for "today": there is no such day inside
	// it to sell on. A caller asking about a real date never produces it.
	if day < 1 {
		return DayTarget{State: DayTargetClosedMonth}
	}
	if day > clock.total {
		return DayTarget{State: DayTargetMonthOver}
	}

	weekday := clock.weekdayOf(day)
	historical := rates.avg[int(weekday)]
	target := DayTarget{
		Basis:      clock.basisFor(day),
		Date:       clock.dateOf(day),
		Day:        weekday,
		Realized:   realized,
		Historical: historical,
	}

	// A day that has closed is reported, not asked, and it keeps its Historical:
	// "essa quarta deu R$ 1.200,00" only has a size next to "uma quarta costuma
	// dar R$ 1.000,00". The reasons the forward days give for having no ask —
	// no goal, no history, goal already met — do not apply to it: none of them
	// stop a day that happened from having happened.
	if target.Basis == DayRealized {
		target.State = DayTargetClosedDay
		target.Delta, target.DeltaPercent, target.Status = compareToUsual(realized, historical)
		return target
	}

	target.State = p.State
	if p.State != DayTargetOK {
		// No amounts under an absence: a "meta: R$ 0,00" reads aloud as a target
		// of nothing. Realized survives — it is fact, not a share of a goal.
		target.Historical = 0
		return target
	}

	// A weekday the shop never opens on is not a day to ask anything of. It is
	// told apart from "no history at all" in newRemainingPlan: one is a fact
	// about the business, the other a gap in the data, and a pharmacy closed on
	// Sundays must not be told every Sunday that it lacks data.
	if historical <= 0 {
		target.State = DayTargetClosedWeekday
		return target
	}

	target.Target = roundToInt64(float64(historical) * p.Factor)
	target.Factor = p.Factor
	target.Source = p.source()
	target.Delta, target.DeltaPercent, target.Status = compareToUsual(target.Target, historical)
	return target
}

// source says what the ask is: a share of what the month still needs, or the
// day's own rhythm holding the floor under it — and, when it is the floor,
// whether there is anything left to chase.
//
// The amounts cannot answer that. Once the plan is floored, an ask equal to the
// weekday average is what a month in perfect step and a month already past its
// goal both produce, and "a meta de hoje é R$ 1.000,00" reads identically in
// each — the first is a month to keep steady, the second one to be told it has
// arrived. Naming it is the same rule DayBasis follows for the other axis.
func (p Plan) source() DayTargetSource {
	switch {
	case p.GapFactor > 1:
		return TargetFromGap
	case p.GapFactor <= 0:
		// The gap is exactly nothing, which newPlan clamps at zero: the goal is
		// met and every remaining day is asked for its rhythm alone.
		return TargetGoalMet
	default:
		return TargetFromAverage
	}
}

// compareToUsual grades an amount against what its weekday usually brings.
//
// The amount is whichever figure the day's basis carries — what it is being
// asked for while it is still ahead, what it actually took once it has closed —
// and that makes the *same* scale mean opposite things on the two sides of
// today. A Wednesday asked for 12% above its usual is a month running behind;
// a Wednesday that took 12% above its usual is a good day. Consumers must name
// the subject before they name the direction: see dayTargetToolPayload, which
// keys them differently, and the bot answer that read "meta 12% acima da média"
// as "estamos com um bom desempenho" ten minutes after midnight, with nothing
// sold yet.
func compareToUsual(amount, historical int64) (int64, float64, DayTargetScale) {
	if historical <= 0 {
		return 0, 0, ""
	}
	delta := amount - historical
	pct := float64(delta) / float64(historical)

	switch {
	case pct > 0.05:
		return delta, pct, PaceAbove
	case pct < -0.05:
		return delta, pct, PaceBelow
	default:
		return delta, pct, PaceOnTrack
	}
}

// DayTargetOn prices one calendar day against the plan this analysis already
// published. It is how any day other than today is reached — the analysis names
// today because ADR-019 needs it to arrive unasked, and everything else is a
// parameter rather than a field.
//
// It works off the assembled Analysis alone, which is the point: the plan, the
// weekday averages and the clock all come back out of what was already
// computed, so a day priced here cannot disagree with the todayTarget sitting
// beside it. It also means a stored snapshot can answer for a day.
//
// realized is what that day sold — fact the caller reads from the ledger, since
// an Analysis carries the month's totals and not its days.
//
// now must be the same instant the analysis was assembled with, and in the same
// location; it is what places the day against the month.
func (a Analysis) DayTargetOn(date domain.CalendarDate, realized int64, now time.Time) DayTarget {
	// A date outside the analysed month is not this month's to price, whichever
	// side it falls on. The tool tells a future month from a past one before it
	// gets here — see DayTargetFutureMonth.
	if date.Format(domain.MonthLayout) != a.Month {
		return DayTarget{State: DayTargetMonthOver}
	}

	var rates dailyRates
	for _, w := range a.Weekdays {
		rates.avg[int(w.Day)] = w.Avg
		rates.weeks[int(w.Day)] = w.Count
	}
	return a.Projection.Plan.at(rates, newMonthClock(a.Month, now), date.Day(), realized)
}
