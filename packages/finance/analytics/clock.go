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
}

// newMonthClock places now inside the analysed month. An unparseable month
// falls back to now's calendar, which is what every caller passes anyway —
// Assemble parses the month before it gets here.
func newMonthClock(month string, now time.Time) monthClock {
	total := daysInMonth(now)
	if _, last, err := domain.ParseMonth(month); err == nil {
		total = last.Day()
	}
	if domain.MonthOf(now) != month {
		return monthClock{through: total, total: total}
	}
	return monthClock{
		inProgress: true,
		through:    now.Day() - 1,
		remaining:  total - now.Day() + 1,
		today:      now.Day(),
		total:      total,
	}
}

// measurable reports whether the month has a finished day to report on. Only
// the first day of a month in progress does not, and on that day every
// retrospective reading — the traffic light, the trends, the month-over-month
// insights — has to say "not yet" instead of quoting a number.
func (c monthClock) measurable() bool { return c.through > 0 }

// period exports the clock for consumers, so the dashboard can label a window
// and the digest can say which days it is talking about.
func (c monthClock) period() Period {
	return Period{
		ThroughDay:    c.through,
		DaysRemaining: c.remaining,
		DaysTotal:     c.total,
		InProgress:    c.inProgress,
	}
}
