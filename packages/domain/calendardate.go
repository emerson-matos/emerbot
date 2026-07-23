package domain

import (
	"encoding/json"
	"time"
)

const calendarLayout = "2006-01-02"

// CalendarDate represents a calendar day, independent of a timezone or instant.
type CalendarDate time.Time

func NewCalendarDate(t time.Time) CalendarDate {
	y, m, d := t.Date()
	return CalendarDate(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

func ParseCalendarDate(s string) (CalendarDate, error) {
	t, err := time.Parse(calendarLayout, s)
	if err != nil {
		return CalendarDate{}, err
	}
	return NewCalendarDate(t), nil
}

func (d CalendarDate) Time() time.Time                { return time.Time(d) }
func (d CalendarDate) UTC() time.Time                 { return d.Time().UTC() }
func (d CalendarDate) String() string                 { return d.Time().Format(calendarLayout) }
func (d CalendarDate) Valid() bool                    { return !d.Time().IsZero() }
func (d CalendarDate) Format(layout string) string    { return d.Time().Format(layout) }
func (d CalendarDate) Equal(other CalendarDate) bool  { return d.Time().Equal(other.Time()) }
func (d CalendarDate) Before(other CalendarDate) bool { return d.Time().Before(other.Time()) }
func (d CalendarDate) After(other CalendarDate) bool  { return d.Time().After(other.Time()) }
func (d CalendarDate) Year() int                      { return d.Time().Year() }
func (d CalendarDate) Month() time.Month              { return d.Time().Month() }
func (d CalendarDate) Day() int                       { return d.Time().Day() }

// MarshalJSON formats the calendar date as "2006-01-02".
func (d CalendarDate) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON parses a "2006-01-02" string into a CalendarDate.
func (d *CalendarDate) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	t, err := time.Parse(calendarLayout, s)
	if err != nil {
		return err
	}
	*d = NewCalendarDate(t)
	return nil
}
