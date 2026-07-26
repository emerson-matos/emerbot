package domain

import (
	"fmt"
	"time"
)

// MonthLayout is how a calendar month is written everywhere in this app: in
// query params, in Goal.Month, and in the summary APIs.
const MonthLayout = "2006-01"

// CurrentMonth returns the current UTC month as "2006-01". Handlers use it to
// default an absent month param — the app is single-timezone, so UTC is the
// one clock everything agrees on.
func CurrentMonth() string { return time.Now().UTC().Format(MonthLayout) }

// MonthOf returns t's month as "2006-01".
func MonthOf(t time.Time) string { return t.UTC().Format(MonthLayout) }

// ParseMonth turns "2006-01" into the month's first and last day (UTC
// midnight, both inclusive). A month it cannot parse is an error rather than
// an empty range: on a financial view, "no data" and "you typed it wrong" must
// not look the same.
func ParseMonth(s string) (from, to time.Time, err error) {
	from, err = time.Parse(MonthLayout, s)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid month %q, expected YYYY-MM: %w", s, err)
	}
	return from, from.AddDate(0, 1, -1), nil
}

// ParseDay parses a "2006-01-02" day. It is the single spelling of that layout
// for callers that want a time.Time rather than a CalendarDate.
func ParseDay(s string) (time.Time, error) {
	t, err := time.Parse(calendarLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q, expected YYYY-MM-DD: %w", s, err)
	}
	return t, nil
}

// CurrentMonthRange returns the first and last day of the current UTC month —
// the default window for every "this month" view.
func CurrentMonthRange() (from, to time.Time) {
	now := time.Now().UTC()
	from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, -1)
}
