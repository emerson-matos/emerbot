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
// It is computed once here. NeededPerDay keeps the second formula — what the
// days left must each bring to reach the target, measured from real
// faturamento, not from a projection — because that is the number a person
// can act on, and every consumer now quotes this one.
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
	// TodayTarget scales today's historical weekday average by the factor
	// computed from the ratio between the remaining revenue target and the sum
	// of historical averages for all remaining calendar days. When the target
	// is already met the factor is zero and Valid stays false — the card is not
	// shown because the question "how much do I need to sell today?" no longer
	// applies.
	if clock.inProgress && basis != ProjectionNoBasis {
		todayWd := int(time.Date(now.Year(), now.Month(), clock.today, 12, 0, 0, 0, now.Location()).Weekday())
		todayAvg := rates.avg[todayWd]

		var totalHistAvg int64
		for day := clock.today; day <= clock.total; day++ {
			d := time.Date(now.Year(), now.Month(), day, 12, 0, 0, 0, now.Location())
			totalHistAvg += rates.avg[int(d.Weekday())]
		}

		// Measured from what the till held when the day opened: totalHistAvg
		// prices today at a whole day's average, so a numerator net of the
		// morning shrank the target hour by hour against a whole-day Histórico.
		missing := projection.Target - (projection.Actual - todayRevenue)
		if totalHistAvg > 0 && todayAvg > 0 && missing > 0 {
			factor := float64(missing) / float64(totalHistAvg)
			todayTarget := roundToInt64(float64(todayAvg) * factor)
			delta := todayTarget - todayAvg

			status := PaceOnTrack
			pct := float64(delta) / float64(todayAvg)
			if pct > 0.05 {
				status = PaceAbove
			} else if pct < -0.05 {
				status = PaceBelow
			}

			projection.TodayTarget = TodayTarget{
				Valid:        true,
				Weekday:      weekdayLabels[todayWd],
				Historical:   todayAvg,
				Target:       todayTarget,
				Delta:        delta,
				DeltaPercent: pct,
				Factor:       factor,
				Status:       status,
			}
		}
	}

	return projection
}
