package analytics

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

	// The per-day asks, assigned exactly once and before the early return below,
	// so that a month with no goal still says *why* it has no ask instead of
	// carrying the zero value of a struct nobody filled in.
	//
	// One plan, priced twice: today's share and tomorrow's come off the same
	// factor, so they add up to the same gap the projection above reports.
	plan := newRemainingPlan(rates, projection, basis, clock, todayRevenue)
	projection.TodayRevenue = todayRevenue
	projection.TodayTarget = plan.at(rates, clock, clock.today)
	projection.NextDayTarget = plan.at(rates, clock, clock.today+1)

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

// remainingPlan is how the month's gap is distributed over the days it has
// left: every remaining day is asked for its own weekday average times one
// shared factor. A factor of 1.08 asks every day for 8% above its usual rhythm.
//
// There is exactly one of these per analysis, and it is what makes today's ask
// and tomorrow's two slices of one plan rather than two rival forecasts. By
// construction the slices sum back to what is missing:
//
//	Σ avg[weekday(d)] × factor, over d in today..end  ==  missing
//
// which is the property a flat daily average never had — see ADR-019 for the
// R$ 1.200 it printed on a Sunday worth R$ 600.
//
// state carries the reason when no day of the month has an ask at all, so a
// caller renders "a meta já foi batida" rather than the same blank space it
// would render for "não há histórico".
type remainingPlan struct {
	state  DayTargetState
	factor float64
}

// newRemainingPlan prices the gap over the days the month has left. The guards
// run in the order a person would ask them: a month that has ended has no day
// left to sell on, a month with no goal has nothing to take a share of, a goal
// already met needs no share, and only then does the history behind the days
// matter.
func newRemainingPlan(rates dailyRates, projection Projection, basis ProjectionBasis, clock monthClock, todayRevenue int64) remainingPlan {
	if !clock.inProgress {
		return remainingPlan{state: DayTargetClosedMonth}
	}
	if projection.Target <= 0 {
		return remainingPlan{state: DayTargetNoGoal}
	}

	// Measured from what the till held when the day opened: totalHistAvg prices
	// today at a whole day's average, so a numerator net of the morning shrank
	// the target hour by hour against a whole-day Histórico.
	missing := projection.Target - (projection.Actual - todayRevenue)
	if missing <= 0 {
		return remainingPlan{state: DayTargetGoalMet}
	}

	var totalHistAvg int64
	for day := clock.today; day <= clock.total; day++ {
		totalHistAvg += rates.avg[int(clock.weekdayOf(day))]
	}
	if basis == ProjectionNoBasis || totalHistAvg <= 0 {
		return remainingPlan{state: DayTargetNoHistory}
	}

	return remainingPlan{state: DayTargetOK, factor: float64(missing) / float64(totalHistAvg)}
}

// at prices one day of the analysed month against the plan: that weekday's own
// average, scaled by the shared factor.
//
// day is a day of the month, and it may be one past its last — that is how the
// caller asks for tomorrow on the 31st, and the answer is that tomorrow belongs
// to a month this analysis knows nothing about. It is named rather than left as
// a silence, because "there is no ask" and "the ask is for a month I cannot see
// from here" are different answers and only one of them is about this pharmacy.
func (p remainingPlan) at(rates dailyRates, clock monthClock, day int) DayTarget {
	// Before the weekday is even computed: a closed month has no clock.today to
	// count from, and the date would land in the previous month.
	if p.state == DayTargetClosedMonth {
		return DayTarget{State: DayTargetClosedMonth}
	}
	if day > clock.total {
		return DayTarget{State: DayTargetMonthOver}
	}

	weekday := clock.weekdayOf(day)
	target := DayTarget{State: p.state, Date: clock.dateOf(day), Day: weekday}
	if p.state != DayTargetOK {
		return target
	}

	// A weekday the shop never opens on is not a day to ask anything of. It is
	// told apart from "no history at all" above: one is a fact about the
	// business, the other a gap in the data, and a pharmacy closed on Sundays
	// must not be told every Sunday that it lacks data.
	historical := rates.avg[int(weekday)]
	if historical <= 0 {
		target.State = DayTargetClosedWeekday
		return target
	}

	target.Historical = historical
	target.Target = roundToInt64(float64(historical) * p.factor)
	target.Delta = target.Target - historical
	target.DeltaPercent = float64(target.Delta) / float64(historical)
	target.Factor = p.factor

	target.Status = PaceOnTrack
	if target.DeltaPercent > 0.05 {
		target.Status = PaceAbove
	} else if target.DeltaPercent < -0.05 {
		target.Status = PaceBelow
	}
	return target
}
