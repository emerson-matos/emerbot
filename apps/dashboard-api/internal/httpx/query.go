package httpx

import (
	"errors"
	"net/http"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// MaxRangeDays bounds a from/to window. Each request becomes a DynamoDB range
// query charged per item read, so an unbounded window ("from=1900-01-01")
// would read the ledger's whole history on a single request. Two years is well
// beyond any dashboard view while keeping the worst case finite.
const MaxRangeDays = 731

// DateRange reads from/to query params (YYYY-MM-DD), defaulting to the current
// calendar month in loc.
//
// Malformed input is an error rather than a silent fallback. The endpoints used
// to disagree about this — /payments/* rejected a bad date while /summary/*
// quietly substituted the current month — so a typo'd date returned a real
// period's numbers under the label the user asked for, which on a financial
// dashboard is indistinguishable from correct data.
func DateRange(r *http.Request, loc *time.Location) (from, to time.Time, err error) {
	from, to = domain.CurrentMonthRange(loc)

	q := r.URL.Query()
	if f := q.Get("from"); f != "" {
		if from, err = domain.ParseDay(f); err != nil {
			return time.Time{}, time.Time{}, errors.New("invalid from date, expected YYYY-MM-DD")
		}
	}
	if t := q.Get("to"); t != "" {
		if to, err = domain.ParseDay(t); err != nil {
			return time.Time{}, time.Time{}, errors.New("invalid to date, expected YYYY-MM-DD")
		}
	}

	if to.Before(from) {
		return time.Time{}, time.Time{}, errors.New("to must not be before from")
	}
	if to.Sub(from) > MaxRangeDays*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("date range too large, max 731 days")
	}
	return from, to, nil
}

// CalendarDateRange is DateRange in the domain's calendar-day type, for
// handlers whose stores are keyed by CalendarDate.
func CalendarDateRange(r *http.Request, loc *time.Location) (from, to domain.CalendarDate, err error) {
	f, t, err := DateRange(r, loc)
	if err != nil {
		return domain.CalendarDate{}, domain.CalendarDate{}, err
	}
	return domain.NewCalendarDate(f), domain.NewCalendarDate(t), nil
}

// Month reads the month query param (YYYY-MM), defaulting to the current
// month in loc. An unparseable month is an error, for the same reason
// DateRange rejects a bad day.
func Month(r *http.Request, loc *time.Location) (string, error) {
	month := r.URL.Query().Get("month")
	if month == "" {
		return domain.CurrentMonth(loc), nil
	}
	if _, _, err := domain.ParseMonth(month); err != nil {
		return "", errors.New("invalid month, expected YYYY-MM")
	}
	return month, nil
}
