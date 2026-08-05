package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// monthClock answers "where in the analysed month are we?" once, so every part
// of the analysis draws the same line between what has already happened and
// what is still ahead. Two rules, and everything else follows from them:
//
//   - what is *measured* ends yesterday. Today is still being traded: at the
//     hour the digest goes out almost nothing has been sold yet, so holding
//     today against a day that finished reports a collapse every morning. On
//     the 1st it reported a 100% fall in receita before the shop had opened.
//   - what is *asked for* starts today. Today is a day the pharmacy can still
//     sell on, so it belongs to the days remaining. The projection used to
//     start at tomorrow, which wrote today off before it happened and, on the
//     last day of the month, announced there was nothing left to recover with.
//
// It is derived from the analysed month rather than from now's month, because
// the dashboard also renders past months: reading now's calendar there gave a
// closed July "faltam 30 dias" when opened in August.
type monthClock struct {
	// inProgress is true when the analysed month is the one Now falls in.
	// A month that is not is already closed and is reported whole.
	inProgress bool
	// through is the last day of the month with complete data: yesterday while
	// the month is running, the month's last day once it is closed. Zero on the
	// first day of a month — nothing has finished yet, and every retrospective
	// figure must stay empty rather than become a percentage of nothing.
	through int
	// remaining counts the days still to trade, today included. Zero for a
	// closed month.
	remaining int
	// today is the day of the month Now falls on, or 0 for a closed month.
	today int
	// total is the number of days in the analysed month.
	total int
	// first is the analysed month's first day, at noon like every other date
	// this package builds. The projection window is measured back from it, so
	// the clock has to know where the month sits on the calendar and not just
	// how long it is.
	first time.Time
}

// newMonthClock places now inside the analysed month. An unparseable month
// falls back to now's calendar, which is what every caller passes anyway —
// Assemble parses the month before it gets here.
func newMonthClock(month string, now time.Time) monthClock {
	total := daysInMonth(now)
	first := time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, now.Location())
	if start, last, err := domain.ParseMonth(month); err == nil {
		total = last.Day()
		first = time.Date(start.Year(), start.Month(), 1, 12, 0, 0, 0, now.Location())
	}
	// now's own calendar, not domain.MonthOf, which reads it in UTC. Input.Now is
	// already in the pharmacy's timezone and every other field here is taken from
	// its local fields, so comparing a UTC month against them disagreed with
	// itself for the last hours of each evening in Brazil: at 22:00 on 31 July,
	// asking for August answered "in progress, today is the 31st", and the
	// projection window that follows from it ran thirty days into the future.
	if now.Format(domain.MonthLayout) != month {
		return monthClock{through: total, total: total, first: first}
	}
	return monthClock{
		inProgress: true,
		through:    now.Day() - 1,
		remaining:  total - now.Day() + 1,
		today:      now.Day(),
		total:      total,
		first:      first,
	}
}

// dayOf places a day of the analysed month on the calendar. It is counted off
// `first`, which is at noon, so a DST jump cannot land the day on its
// neighbour — and off the *analysed* month rather than now's, which is what
// lets a closed July be read in August without every date sliding a month.
//
// day may run one past the month's last, which is how a caller asks about
// tomorrow on the 31st; the result is then the 1st of the next month, and it is
// the caller's job to decide whether that is a day it can say anything about.
func (c monthClock) dayOf(day int) time.Time { return c.first.AddDate(0, 0, day-1) }

// weekdayOf is which day of the week a day of the analysed month falls on —
// the index every per-weekday average is read at.
func (c monthClock) weekdayOf(day int) time.Weekday { return c.dayOf(day).Weekday() }

// dateOf is a day of the analysed month as a calendar date, "YYYY-MM-DD", for
// consumers that name the day they are talking about rather than counting it.
func (c monthClock) dateOf(day int) string { return c.dayOf(day).Format("2006-01-02") }

// basisFor places a day of the analysed month on the same line every other
// reading here is placed on: finished, being traded, or still ahead. It is the
// clock's to answer because the clock is where that line is decided once
// (ADR-017), and a second opinion about which side of it a day falls on is
// exactly how the analysis used to contradict itself.
//
// A month that is not the one Now falls in is treated as entirely finished.
// That is true of a past month and false of a future one, and this cannot tell
// them apart — monthClock does not carry Now. Callers that can be handed a
// future month must reject it first; see the get_meta_do_dia tool, which
// answers DayTargetFutureMonth without assembling anything at all.
func (c monthClock) basisFor(day int) DayBasis {
	switch {
	case !c.inProgress || day < c.today:
		return DayRealized
	case day == c.today:
		return DayInProgress
	default:
		return DayProjected
	}
}

// projectionWindowWeeks is how far back the projection looks to learn what a
// day of the week is worth. Whole weeks rather than a round number of days, so
// every weekday gets exactly the same number of chances to be observed: over 60
// days four weekdays would come up nine times and three only eight.
//
// Eight of them is roughly two months — enough that one unusual Tuesday moves
// the Tuesday rate by an eighth rather than a quarter, and short enough to
// follow the pharmacy when its trading level actually shifts.
const projectionWindowWeeks = 8

// projectionWindow is the stretch of days the per-weekday rates are averaged
// over, inclusive at both ends.
//
// It ends on the last day with complete data *relative to the analysed month* —
// yesterday while the month runs, the day before the month started on its first
// day, the month's own last day once it is closed. Never on "today": the
// dashboard renders past months too, and a window counted off the clock would
// price a closed July from August's sales.
//
// This is what stops the start of a month from projecting nothing. Reading only
// the month's own finished days, a 3 August had seen one Saturday and one
// Sunday, so the twenty-one weekdays still to come were each projected at zero
// and the month's projection came in at a quarter of the goal.
func (c monthClock) projectionWindow() (from, to domain.CalendarDate) {
	// through counts finished days, so first+through-1 is the last of them;
	// through == 0 lands on the day before the month began, which is exactly
	// what "nothing of this month has finished" should look back to.
	end := c.first.AddDate(0, 0, c.through-1)
	start := end.AddDate(0, 0, -(projectionWindowWeeks*daysInWeek - 1))
	return domain.NewCalendarDate(start), domain.NewCalendarDate(end)
}

// measurable reports whether the month has a finished day to report on. Only
// the first day of a month in progress does not, and on that day every
// retrospective reading — the traffic light, the trends, the month-over-month
// insights — has to say "not yet" instead of quoting a number.
func (c monthClock) measurable() bool { return c.through > 0 }

// comparableThrough is the last day of the month the *month-over-month*
// comparison may measure through — whole weeks from the 1st while the month
// runs, the month's own last day once it is closed. Zero for the first week of
// a month, which has nothing to compare yet.
//
// It is a different line from `through`, and the difference is the whole point.
// `through` answers "what has happened here"; this answers "what can honestly
// be held against last month". A window of N consecutive days starting on the
// 1st carries the same mix of weekdays on both sides only when N is a multiple
// of seven — any other length gives one month more Saturdays than the other,
// and the percentage then measures the calendar rather than the pharmacy.
//
// On 3 August 2026 the comparison ran the 1st and 2nd — a Saturday and a
// Sunday — against the 1st and 2nd of July, a Wednesday and a Thursday. A
// pharmacy that had traded both weekend days at exactly its usual weekend rate
// was told "Receita caiu 53% abaixo do mês passado" and given a red alert. That
// is not a small sample being honest; it is a biased one, and it fires in the
// first week of every single month.
//
// The cost is that the figure moves in weekly steps rather than daily ones —
// between the 8th and the 13th it stays on days 1–7. That is affordable
// precisely because the day-to-day reading already exists and is already
// weekday-aligned: WeekPace compares this week against last week over the same
// weekdays. The month-over-month reading is about the month, and it can wait
// for a week to close.
func (c monthClock) comparableThrough() int {
	if !c.inProgress {
		return c.through
	}
	return c.through - c.through%daysInWeek
}

// elapsed is the last day of the month that has arrived, today included — the
// month's last day once it is closed. It is a third line, and the one the KPI
// row stands on: those cards are the state of the month *now*, not a
// measurement of it, so unlike `through` they do count today. A pharmacist
// looking at the page at six in the evening is owed the day's takings.
//
// What they are emphatically not is the whole month. The KPIs used to be read
// straight off MonthlySummary, which totals every entry that *falls in* the
// month — including the rent due on the 25th, booked on the 1st. Faturamento and
// entradas de caixa cannot contain a future day, so two cards of the row were
// "so far" and two were "everything scheduled", and Resultado subtracted one
// kind from the other: on 3 August, R$ 16.200,00 of despesa against R$ 200,00
// actually spent, and a resultado of minus R$ 14.800,00 for a month running
// R$ 1.200,00 up.
func (c monthClock) elapsed() int {
	if !c.inProgress {
		return c.total
	}
	return c.today
}

// period exports the clock for consumers, so the dashboard can label a window
// and the digest can say which days it is talking about.
func (c monthClock) period() Period {
	return Period{
		ThroughDay:           c.through,
		ComparableThroughDay: c.comparableThrough(),
		DaysRemaining:        c.remaining,
		DaysTotal:            c.total,
		InProgress:           c.inProgress,
	}
}
