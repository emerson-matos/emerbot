package domain

import (
	"testing"
	"time"
)

func TestParseMonth(t *testing.T) {
	from, to, err := ParseMonth("2026-07")
	if err != nil {
		t.Fatalf("ParseMonth: %v", err)
	}
	if from.Format(calendarLayout) != "2026-07-01" {
		t.Fatalf("from = %s, want 2026-07-01", from.Format(calendarLayout))
	}
	// The last day, not the first of the next month — the range is inclusive.
	if to.Format(calendarLayout) != "2026-07-31" {
		t.Fatalf("to = %s, want 2026-07-31", to.Format(calendarLayout))
	}

	t.Run("month lengths", func(t *testing.T) {
		cases := map[string]string{
			"2026-02": "2026-02-28",
			"2024-02": "2024-02-29", // leap year
			"2026-04": "2026-04-30",
			"2026-12": "2026-12-31",
		}
		for month, wantLast := range cases {
			_, last, err := ParseMonth(month)
			if err != nil {
				t.Fatalf("ParseMonth(%q): %v", month, err)
			}
			if got := last.Format(calendarLayout); got != wantLast {
				t.Fatalf("ParseMonth(%q) last day = %s, want %s", month, got, wantLast)
			}
		}
	})
}

func TestParseMonthRejectsBadInput(t *testing.T) {
	// A month the user typed wrong must be an error, never a silently empty
	// range that renders as R$ 0,00.
	for _, in := range []string{"", "julho", "2026", "2026-13", "2026-07-01", "26-07"} {
		if _, _, err := ParseMonth(in); err == nil {
			t.Fatalf("ParseMonth(%q) returned no error", in)
		}
	}
}

func TestParseDay(t *testing.T) {
	got, err := ParseDay("2026-07-20")
	if err != nil {
		t.Fatalf("ParseDay: %v", err)
	}
	if got.Format(calendarLayout) != "2026-07-20" {
		t.Fatalf("ParseDay = %s, want 2026-07-20", got.Format(calendarLayout))
	}

	for _, in := range []string{"", "20/07/2026", "2026-07", "hoje", "2026-13-01"} {
		if _, err := ParseDay(in); err == nil {
			t.Fatalf("ParseDay(%q) returned no error", in)
		}
	}
}

func TestCurrentMonthAndMonthOf(t *testing.T) {
	if got := CurrentMonth(); got != time.Now().UTC().Format(MonthLayout) {
		t.Fatalf("CurrentMonth = %q, want the current UTC month", got)
	}
	if got := MonthOf(time.Date(2026, 7, 20, 23, 0, 0, 0, time.UTC)); got != "2026-07" {
		t.Fatalf("MonthOf = %q, want 2026-07", got)
	}
	// A non-UTC instant is normalised before formatting, so a late-evening
	// local time cannot report the previous month.
	tz := time.FixedZone("UTC-3", -3*3600)
	if got := MonthOf(time.Date(2026, 8, 1, 1, 0, 0, 0, tz)); got != "2026-08" {
		t.Fatalf("MonthOf = %q, want 2026-08 after normalising to UTC", got)
	}
}

func TestAddMonthsClampsToTheLastDayOfTheMonth(t *testing.T) {
	cases := []struct {
		name string
		from string
		n    int
		want string
	}{
		{"same day when the target month is long enough", "2026-07-15", 1, "2026-08-15"},
		{"31st into a 30-day month", "2026-07-31", 3, "2026-10-31"},
		{"31st into a 31-day month stays", "2026-07-31", 1, "2026-08-31"},
		{"31st clamps to the 30th", "2026-08-31", 1, "2026-09-30"},
		{"31st clamps to the end of February", "2026-07-31", 7, "2027-02-28"},
		{"29 February clamps in a common year", "2024-02-29", 12, "2025-02-28"},
		{"29 February survives a leap year", "2024-02-29", 48, "2028-02-29"},
		{"negative shifts backwards", "2026-07-31", -1, "2026-06-30"},
		{"zero is the identity", "2026-07-31", 0, "2026-07-31"},
		{"crossing the year backwards", "2026-01-31", -2, "2025-11-30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, err := ParseDay(tc.from)
			if err != nil {
				t.Fatalf("ParseDay(%q): %v", tc.from, err)
			}
			if got := AddMonths(from, tc.n).Format(calendarLayout); got != tc.want {
				t.Fatalf("AddMonths(%s, %d) = %s, want %s", tc.from, tc.n, got, tc.want)
			}
		})
	}
}

// AddMonths must move by exactly n months, whatever the starting day —
// AddDate's overflow silently skips one.
func TestAddMonthsAlwaysMovesExactlyNMonths(t *testing.T) {
	start := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	for n := range 36 {
		got := AddMonths(start, n)
		months := (got.Year()-start.Year())*12 + int(got.Month()-start.Month())
		if months != n {
			t.Fatalf("AddMonths(%s, %d) = %s, which is %d months away", start.Format(calendarLayout), n, got.Format(calendarLayout), months)
		}
	}
}

// A day-of-month clock reading is a calendar day, so the time of day and the
// zone of the input must not leak into the result.
func TestAddMonthsNormalisesToUTCMidnight(t *testing.T) {
	tz := time.FixedZone("UTC-3", -3*3600)
	got := AddMonths(time.Date(2026, 7, 31, 23, 30, 0, 0, tz), 1)
	if want := "2026-09-01"; got.Format(calendarLayout) != want {
		t.Fatalf("AddMonths = %s, want %s (1 August in UTC, plus a month)", got.Format(calendarLayout), want)
	}
	if h, m, s := got.Clock(); h|m|s != 0 || got.Location() != time.UTC {
		t.Fatalf("AddMonths = %v, want UTC midnight", got)
	}
}

func TestCurrentMonthRange(t *testing.T) {
	from, to := CurrentMonthRange()
	if from.Day() != 1 {
		t.Fatalf("from = %v, want the first of the month", from)
	}
	if MonthOf(from) != CurrentMonth() || MonthOf(to) != CurrentMonth() {
		t.Fatalf("range %v..%v spans outside the current month", from, to)
	}
	if !to.After(from) {
		t.Fatalf("range %v..%v is not ordered", from, to)
	}
}
