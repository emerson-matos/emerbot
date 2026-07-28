package analytics

import "time"

// buildProjection projects where the month's revenue lands if the days left
// trade like the same weekdays already have, and prices the gap to the
// revenue goal.
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
// days left must each bring to reach the target, measured from real revenue,
// not from a projection — because that is the number a person can act on, and
// every consumer now quotes this one.
func buildProjection(weekdays []WeekdayStat, goals GoalProgress, now time.Time) Projection {
	projection := Projection{
		Actual:        goals.RevenueActual,
		Projected:     goals.RevenueActual,
		Target:        goals.RevenueTarget,
		DaysRemaining: goals.DaysRemaining,
	}

	avgByWeekday := make(map[int]int64, len(weekdays))
	for _, w := range weekdays {
		avgByWeekday[w.Day] = w.Avg
	}

	// Noon, like every other date this package builds, so a DST jump cannot
	// land the day on its neighbour.
	lastDay := daysInMonth(now)
	for day := now.Day() + 1; day <= lastDay; day++ {
		d := time.Date(now.Year(), now.Month(), day, 12, 0, 0, 0, now.Location())
		projection.Remaining += avgByWeekday[int(d.Weekday())]
	}
	projection.Projected += projection.Remaining

	if projection.Target <= 0 {
		return projection
	}

	// The verdict stands on the last day of the month too — the month either
	// reached its target or it did not. Only the per-day ask needs days left to
	// spread over.
	projection.OnTrack = projection.Projected >= projection.Target
	if gap := projection.Target - projection.Projected; gap > 0 {
		projection.Gap = gap
	}
	if projection.Pacing() {
		if missing := projection.Target - projection.Actual; missing > 0 {
			projection.NeededPerDay = roundToInt64(float64(missing) / float64(projection.DaysRemaining))
		}
	}
	return projection
}
