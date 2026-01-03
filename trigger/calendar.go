package trigger

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"time"
)

// CalendarIntervalTrigger fires at calendar-based intervals (years, months, weeks, days)
// always at the same time of day.
type CalendarIntervalTrigger struct {
	Years     int
	Months    int
	Weeks     int
	Days      int
	Hour      int
	Minute    int
	Second    int
	StartDate time.Time // Only the date portion is used
	EndDate   *time.Time
	Location  *time.Location
	Jitter    time.Duration
}

// CalendarOption configures a CalendarIntervalTrigger.
type CalendarOption func(*CalendarIntervalTrigger)

// WithCalendarYears sets the years interval.
func WithCalendarYears(years int) CalendarOption {
	return func(t *CalendarIntervalTrigger) { t.Years = years }
}

// WithCalendarMonths sets the months interval.
func WithCalendarMonths(months int) CalendarOption {
	return func(t *CalendarIntervalTrigger) { t.Months = months }
}

// WithCalendarWeeks sets the weeks interval.
func WithCalendarWeeks(weeks int) CalendarOption {
	return func(t *CalendarIntervalTrigger) { t.Weeks = weeks }
}

// WithCalendarDays sets the days interval.
func WithCalendarDays(days int) CalendarOption {
	return func(t *CalendarIntervalTrigger) { t.Days = days }
}

// WithCalendarTime sets the time of day to fire.
func WithCalendarTime(hour, minute, second int) CalendarOption {
	return func(t *CalendarIntervalTrigger) {
		t.Hour = hour
		t.Minute = minute
		t.Second = second
	}
}

// WithCalendarStartDate sets the start date.
func WithCalendarStartDate(d time.Time) CalendarOption {
	return func(t *CalendarIntervalTrigger) { t.StartDate = d }
}

// WithCalendarEndDate sets the end date.
func WithCalendarEndDate(d time.Time) CalendarOption {
	return func(t *CalendarIntervalTrigger) { t.EndDate = &d }
}

// WithCalendarLocation sets the timezone.
func WithCalendarLocation(loc *time.Location) CalendarOption {
	return func(t *CalendarIntervalTrigger) { t.Location = loc }
}

// WithCalendarJitter sets the jitter.
func WithCalendarJitter(d time.Duration) CalendarOption {
	return func(t *CalendarIntervalTrigger) { t.Jitter = d }
}

// NewCalendarIntervalTrigger creates a new CalendarIntervalTrigger.
func NewCalendarIntervalTrigger(opts ...CalendarOption) (*CalendarIntervalTrigger, error) {
	t := &CalendarIntervalTrigger{
		Location:  time.Local,
		StartDate: time.Now(),
	}

	for _, opt := range opts {
		opt(t)
	}

	// Validate that at least one interval is set
	if t.Years == 0 && t.Months == 0 && t.Weeks == 0 && t.Days == 0 {
		return nil, fmt.Errorf("interval must be at least 1 day long")
	}

	// Validate end date
	if t.EndDate != nil {
		startOnly := time.Date(t.StartDate.Year(), t.StartDate.Month(), t.StartDate.Day(), 0, 0, 0, 0, t.Location)
		endOnly := time.Date(t.EndDate.Year(), t.EndDate.Month(), t.EndDate.Day(), 0, 0, 0, 0, t.Location)
		if endOnly.Before(startOnly) {
			return nil, fmt.Errorf("end_date cannot be earlier than start_date")
		}
	}

	return t, nil
}

// GetNextFireTime calculates the next fire time.
func (t *CalendarIntervalTrigger) GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time {
	for {
		var nextDate time.Time

		if previousFireTime != nil {
			year := previousFireTime.Year()
			month := int(previousFireTime.Month())
			day := previousFireTime.Day()

			// Add years and months
			for {
				month += t.Months
				year += t.Years + (month-1)/12
				month = (month-1)%12 + 1
				if month <= 0 {
					month += 12
					year--
				}

				// Try to create the date with the same day
				nextDate = time.Date(year, time.Month(month), day, 0, 0, 0, 0, t.Location)

				// Check if the date is valid (day didn't overflow to next month)
				if nextDate.Day() == day {
					// Add weeks and days
					nextDate = nextDate.AddDate(0, 0, t.Days+t.Weeks*7)
					break
				}
				// Day doesn't exist in this month, try next interval
			}
		} else {
			nextDate = time.Date(t.StartDate.Year(), t.StartDate.Month(), t.StartDate.Day(),
				0, 0, 0, 0, t.Location)
		}

		// Check end date
		if t.EndDate != nil {
			endDate := time.Date(t.EndDate.Year(), t.EndDate.Month(), t.EndDate.Day(),
				0, 0, 0, 0, t.Location)
			if nextDate.After(endDate) {
				return nil
			}
		}

		// Combine date with time
		nextTime := time.Date(nextDate.Year(), nextDate.Month(), nextDate.Day(),
			t.Hour, t.Minute, t.Second, 0, t.Location)

		// Check for DST issues - if the hour changed, skip to next interval
		if nextTime.Hour() != t.Hour {
			// Time was adjusted due to DST, try next interval
			prevFT := time.Date(nextDate.Year(), nextDate.Month(), nextDate.Day(),
				0, 0, 0, 0, t.Location)
			previousFireTime = &prevFT
			continue
		}

		// Apply jitter
		result := ApplyJitter(&nextTime, t.Jitter, now)
		return result
	}
}

// String returns a string representation of the trigger.
func (t *CalendarIntervalTrigger) String() string {
	var parts []string
	if t.Years > 0 {
		parts = append(parts, fmt.Sprintf("years=%d", t.Years))
	}
	if t.Months > 0 {
		parts = append(parts, fmt.Sprintf("months=%d", t.Months))
	}
	if t.Weeks > 0 {
		parts = append(parts, fmt.Sprintf("weeks=%d", t.Weeks))
	}
	if t.Days > 0 {
		parts = append(parts, fmt.Sprintf("days=%d", t.Days))
	}
	parts = append(parts, fmt.Sprintf("time=%02d:%02d:%02d", t.Hour, t.Minute, t.Second))

	return fmt.Sprintf("calendar[%s]", joinParts(parts))
}

func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}

// GobEncode implements gob.GobEncoder for serialization.
func (t *CalendarIntervalTrigger) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	if err := enc.Encode(t.Years); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.Months); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.Weeks); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.Days); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.Hour); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.Minute); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.Second); err != nil {
		return nil, err
	}

	// Start date
	if err := enc.Encode(t.StartDate.Unix()); err != nil {
		return nil, err
	}

	// End date
	hasEnd := t.EndDate != nil
	if err := enc.Encode(hasEnd); err != nil {
		return nil, err
	}
	if hasEnd {
		if err := enc.Encode(t.EndDate.Unix()); err != nil {
			return nil, err
		}
	}

	// Location and jitter
	if err := enc.Encode(t.Location.String()); err != nil {
		return nil, err
	}
	if err := enc.Encode(int64(t.Jitter)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder for deserialization.
func (t *CalendarIntervalTrigger) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	if err := dec.Decode(&t.Years); err != nil {
		return err
	}
	if err := dec.Decode(&t.Months); err != nil {
		return err
	}
	if err := dec.Decode(&t.Weeks); err != nil {
		return err
	}
	if err := dec.Decode(&t.Days); err != nil {
		return err
	}
	if err := dec.Decode(&t.Hour); err != nil {
		return err
	}
	if err := dec.Decode(&t.Minute); err != nil {
		return err
	}
	if err := dec.Decode(&t.Second); err != nil {
		return err
	}

	var startSec int64
	if err := dec.Decode(&startSec); err != nil {
		return err
	}

	var hasEnd bool
	if err := dec.Decode(&hasEnd); err != nil {
		return err
	}
	if hasEnd {
		var endSec int64
		if err := dec.Decode(&endSec); err != nil {
			return err
		}
		endDate := time.Unix(endSec, 0)
		t.EndDate = &endDate
	}

	var locName string
	if err := dec.Decode(&locName); err != nil {
		return err
	}
	loc, err := time.LoadLocation(locName)
	if err != nil {
		loc = time.UTC
	}
	t.Location = loc

	t.StartDate = time.Unix(startSec, 0).In(loc)
	if t.EndDate != nil {
		endDate := t.EndDate.In(loc)
		t.EndDate = &endDate
	}

	var jitterNs int64
	if err := dec.Decode(&jitterNs); err != nil {
		return err
	}
	t.Jitter = time.Duration(jitterNs)

	return nil
}

func init() {
	gob.Register(&CalendarIntervalTrigger{})
}
