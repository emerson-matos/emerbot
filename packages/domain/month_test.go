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
