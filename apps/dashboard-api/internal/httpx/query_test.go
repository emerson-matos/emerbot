package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func request(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, target, nil)
}

func TestDateRangeDefaultsToTheCurrentMonth(t *testing.T) {
	from, to, err := DateRange(request("/x"))
	if err != nil {
		t.Fatalf("DateRange: %v", err)
	}
	wantFrom, wantTo := domain.CurrentMonthRange()
	if !from.Equal(wantFrom) || !to.Equal(wantTo) {
		t.Fatalf("range = %v..%v, want %v..%v", from, to, wantFrom, wantTo)
	}
}

func TestDateRangeReadsBothParams(t *testing.T) {
	from, to, err := DateRange(request("/x?from=2026-07-01&to=2026-07-31"))
	if err != nil {
		t.Fatalf("DateRange: %v", err)
	}
	if from.Format("2006-01-02") != "2026-07-01" || to.Format("2006-01-02") != "2026-07-31" {
		t.Fatalf("range = %v..%v, want the requested days", from, to)
	}
}

func TestDateRangeRejectsBadInput(t *testing.T) {
	// A typo'd date must not fall back to a real period: on a financial
	// dashboard that renders as correct data under the wrong label.
	cases := map[string]string{
		"unparseable from": "/x?from=julho",
		"unparseable to":   "/x?to=agosto",
		"day-month-year":   "/x?from=23-07-2026",
		"inverted":         "/x?from=2026-07-31&to=2026-07-01",
		"too large":        "/x?from=1900-01-01&to=2999-12-31",
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := DateRange(request(target)); err == nil {
				t.Fatalf("DateRange(%q) returned no error", target)
			}
		})
	}
}

func TestDateRangeAllowsExactlyTheMaximumWindow(t *testing.T) {
	from := time.Now().UTC().Truncate(24 * time.Hour)
	to := from.Add(MaxRangeDays * 24 * time.Hour)
	target := "/x?from=" + from.Format("2006-01-02") + "&to=" + to.Format("2006-01-02")

	if _, _, err := DateRange(request(target)); err != nil {
		t.Fatalf("a window of exactly %d days was rejected: %v", MaxRangeDays, err)
	}
}

func TestCalendarDateRange(t *testing.T) {
	from, to, err := CalendarDateRange(request("/x?from=2026-07-01&to=2026-07-31"))
	if err != nil {
		t.Fatalf("CalendarDateRange: %v", err)
	}
	if from.String() != "2026-07-01" || to.String() != "2026-07-31" {
		t.Fatalf("range = %s..%s, want 2026-07-01..2026-07-31", from, to)
	}
	if _, _, err := CalendarDateRange(request("/x?from=julho")); err == nil {
		t.Fatal("expected the underlying validation to apply")
	}
}

func TestMonth(t *testing.T) {
	got, err := Month(request("/x?month=2026-07"))
	if err != nil || got != "2026-07" {
		t.Fatalf("Month = %q (err %v), want 2026-07", got, err)
	}

	got, err = Month(request("/x"))
	if err != nil || got != domain.CurrentMonth() {
		t.Fatalf("Month = %q (err %v), want the current month", got, err)
	}

	for _, bad := range []string{"julho", "2026", "2026-13", "2026-07-01"} {
		if _, err := Month(request("/x?month=" + bad)); err == nil {
			t.Fatalf("Month(%q) returned no error", bad)
		}
	}
}
