package analytics

import "time"

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
// Monday is a number nobody can hit. TodayTarget replaces it — today's share of
// the gap, at today's own weekday rhythm. See ADR-019.
//
// todayRevenue is what has already been sold today; it is subtracted from
// today's own weekday average so the day is not counted twice — once inside
// Actual, once again as a day still expected to bring a full day's takings.
//
// rates price the days still to come, and they come from the trailing weeks
// rather than from the analysed month — see projectionRates for why a month's
// own first days cannot price themselves.
func buildProjection(rates dailyRates, goals GoalProgress, now time.Time, clock monthClock, todayRevenue int64) Projection {
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
	//
	// Noon, like every other date this package builds, so a DST jump cannot
	// land the day on its neighbour.
	for day := clock.today; clock.inProgress && day <= clock.total; day++ {
		d := time.Date(now.Year(), now.Month(), day, 12, 0, 0, 0, now.Location())
		avg := rates.avg[int(d.Weekday())]
		if day == clock.today {
			// Only the part of an ordinary day that has not happened yet is
			// still ahead of us.
			avg = max(0, avg-todayRevenue)
		}
		projection.Remaining += avg
	}
	projection.Projected += projection.Remaining

	// The day's ask, assigned exactly once and before the early return below, so
	// that a month with no goal still says *why* it has no ask instead of
	// carrying the zero value of a struct nobody filled in.
	projection.TodayTarget = todayTarget(rates, projection, basis, now, clock, todayRevenue)

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

// todayTarget scales today's own weekday average by the factor that would close
// what is still missing if every remaining day pulled its own weight. It is the
// only per-day ask this package produces — see ADR-019 for why the flat one it
// replaced (what is missing, over the days left) cannot be given to anyone.
//
// When there is no ask it says which reason, rather than returning an empty
// struct that means six different things. The guards run in the order a person
// would ask them: a month that has ended has no today at all, a month with no
// goal has nothing to take a share of, a goal already met needs no share, and
// only then does the history behind the day matter.
func todayTarget(rates dailyRates, projection Projection, basis ProjectionBasis, now time.Time, clock monthClock, todayRevenue int64) TodayTarget {
	// Before the weekday is even computed: a closed month has no clock.today to
	// read one from, and Day would land on the previous month's last day.
	if !clock.inProgress {
		return TodayTarget{State: TodayTargetClosedMonth}
	}
	weekday := time.Date(now.Year(), now.Month(), clock.today, 12, 0, 0, 0, now.Location()).Weekday()

	if projection.Target <= 0 {
		return TodayTarget{State: TodayTargetNoGoal, Day: weekday}
	}

	// Measured from what the till held when the day opened: totalHistAvg prices
	// today at a whole day's average, so a numerator net of the morning shrank
	// the target hour by hour against a whole-day Histórico.
	missing := projection.Target - (projection.Actual - todayRevenue)
	if missing <= 0 {
		return TodayTarget{State: TodayTargetGoalMet, Day: weekday}
	}

	var totalHistAvg int64
	for day := clock.today; day <= clock.total; day++ {
		d := time.Date(now.Year(), now.Month(), day, 12, 0, 0, 0, now.Location())
		totalHistAvg += rates.avg[int(d.Weekday())]
	}
	if basis == ProjectionNoBasis || totalHistAvg <= 0 {
		return TodayTarget{State: TodayTargetNoHistory, Day: weekday}
	}

	// A weekday the shop never opens on is not a day to ask anything of. It is
	// told apart from "no history at all" above: one is a fact about the
	// business, the other a gap in the data, and a pharmacy closed on Sundays
	// must not be told every Sunday that it lacks data.
	todayAvg := rates.avg[int(weekday)]
	if todayAvg <= 0 {
		return TodayTarget{State: TodayTargetClosedWeekday, Day: weekday}
	}

	factor := float64(missing) / float64(totalHistAvg)
	target := roundToInt64(float64(todayAvg) * factor)
	delta := target - todayAvg

	status := PaceOnTrack
	pct := float64(delta) / float64(todayAvg)
	if pct > 0.05 {
		status = PaceAbove
	} else if pct < -0.05 {
		status = PaceBelow
	}

	return TodayTarget{
		State:        TodayTargetOK,
		Day:          weekday,
		Historical:   todayAvg,
		Target:       target,
		Delta:        delta,
		DeltaPercent: pct,
		Factor:       factor,
		Status:       status,
	}
}
