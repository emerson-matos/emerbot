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

// newPlan distributes the gap over the days the month has left. The guards run
// in the order a person would ask them: a month that has ended has no day left
// to sell on, a month with no goal has nothing to take a share of, a goal
// already met needs no share, and only then does the history behind the days
// matter.
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

	// Measured from what the till held when the day opened: totalHistAvg prices
	// today at a whole day's average, so a numerator net of the morning shrank
	// the target hour by hour against a whole-day Histórico.
	missing := projection.Target - (projection.Actual - todayRevenue)
	if missing <= 0 {
		return Plan{State: DayTargetGoalMet}
	}

	var totalHistAvg int64
	for day := clock.today; day <= clock.total; day++ {
		totalHistAvg += rates.avg[int(clock.weekdayOf(day))]
	}
	if basis == ProjectionNoBasis || totalHistAvg <= 0 {
		return Plan{State: DayTargetNoHistory}
	}

	return Plan{State: DayTargetOK, Factor: float64(missing) / float64(totalHistAvg)}
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
		target.compare(realized)
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
	target.compare(target.Target)
	return target
}

// compare grades a day's figure against what its weekday usually brings. Which
// figure that is follows the basis: what the day is being asked for while it is
// still ahead, what it actually took once it has closed. Both answer the same
// question — is this a heavy day or a light one for this weekday — and having
// one pair of fields for it keeps a consumer from needing to know which case it
// is holding before it can draw a bar.
func (t *DayTarget) compare(amount int64) {
	if t.Historical <= 0 {
		return
	}
	t.Delta = amount - t.Historical
	t.DeltaPercent = float64(t.Delta) / float64(t.Historical)

	t.Status = PaceOnTrack
	if t.DeltaPercent > 0.05 {
		t.Status = PaceAbove
	} else if t.DeltaPercent < -0.05 {
		t.Status = PaceBelow
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
