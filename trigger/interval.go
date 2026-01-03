package trigger

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"math"
	"time"
)

// IntervalTrigger triggers at fixed intervals.
type IntervalTrigger struct {
	Interval  time.Duration
	StartDate time.Time
	EndDate   *time.Time
	Location  *time.Location
	Jitter    time.Duration
}

// IntervalOption configures an IntervalTrigger.
type IntervalOption func(*IntervalTrigger)

// WithIntervalStartDate sets the start date for the interval calculation.
func WithIntervalStartDate(t time.Time) IntervalOption {
	return func(it *IntervalTrigger) {
		it.StartDate = t
	}
}

// WithIntervalEndDate sets the end date after which no more triggers occur.
func WithIntervalEndDate(t time.Time) IntervalOption {
	return func(it *IntervalTrigger) {
		it.EndDate = &t
	}
}

// WithIntervalLocation sets the timezone for the trigger.
func WithIntervalLocation(loc *time.Location) IntervalOption {
	return func(it *IntervalTrigger) {
		it.Location = loc
	}
}

// WithIntervalJitter sets the maximum jitter to add to each fire time.
func WithIntervalJitter(d time.Duration) IntervalOption {
	return func(it *IntervalTrigger) {
		it.Jitter = d
	}
}

// NewIntervalTrigger creates a new IntervalTrigger with the given interval.
// If the interval is zero or negative, it defaults to 1 second.
func NewIntervalTrigger(interval time.Duration, opts ...IntervalOption) *IntervalTrigger {
	t := &IntervalTrigger{
		Interval: interval,
		Location: time.Local,
	}

	// Ensure minimum interval of 1 second
	if t.Interval <= 0 {
		t.Interval = time.Second
	}

	// Apply options
	for _, opt := range opts {
		opt(t)
	}

	// If no start date, set to now + interval
	if t.StartDate.IsZero() {
		t.StartDate = time.Now().In(t.Location).Add(t.Interval)
	}

	return t
}

// NewIntervalTriggerFromParts creates an IntervalTrigger from individual time components.
func NewIntervalTriggerFromParts(weeks, days, hours, minutes, seconds int, opts ...IntervalOption) *IntervalTrigger {
	interval := time.Duration(weeks)*7*24*time.Hour +
		time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second

	return NewIntervalTrigger(interval, opts...)
}

// GetNextFireTime calculates the next fire time based on the interval.
func (t *IntervalTrigger) GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time {
	var nextFireTime time.Time

	if previousFireTime != nil {
		// Add interval to previous fire time
		nextFireTime = previousFireTime.Add(t.Interval)
	} else if t.StartDate.After(now) {
		// Start date is in the future
		nextFireTime = t.StartDate
	} else {
		// Calculate the next interval from start date
		diff := now.Sub(t.StartDate)
		intervals := math.Ceil(float64(diff) / float64(t.Interval))
		nextFireTime = t.StartDate.Add(time.Duration(intervals) * t.Interval)
	}

	// Apply jitter
	if t.Jitter > 0 {
		nextFireTime = *ApplyJitter(&nextFireTime, t.Jitter, now)
	}

	// Check end date
	if t.EndDate != nil && nextFireTime.After(*t.EndDate) {
		return nil
	}

	return &nextFireTime
}

// SetJitter sets the jitter duration.
func (t *IntervalTrigger) SetJitter(d time.Duration) {
	t.Jitter = d
}

// GetJitter returns the jitter duration.
func (t *IntervalTrigger) GetJitter() time.Duration {
	return t.Jitter
}

// String returns a string representation of the trigger.
func (t *IntervalTrigger) String() string {
	return fmt.Sprintf("interval[%s]", t.Interval)
}

// GobEncode implements gob.GobEncoder for serialization.
func (t *IntervalTrigger) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	// Encode interval
	if err := enc.Encode(int64(t.Interval)); err != nil {
		return nil, err
	}

	// Encode start date
	if err := enc.Encode(t.StartDate.Unix()); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.StartDate.Nanosecond()); err != nil {
		return nil, err
	}

	// Encode end date (as bool + timestamp if present)
	hasEndDate := t.EndDate != nil
	if err := enc.Encode(hasEndDate); err != nil {
		return nil, err
	}
	if hasEndDate {
		if err := enc.Encode(t.EndDate.Unix()); err != nil {
			return nil, err
		}
		if err := enc.Encode(t.EndDate.Nanosecond()); err != nil {
			return nil, err
		}
	}

	// Encode location
	if err := enc.Encode(t.Location.String()); err != nil {
		return nil, err
	}

	// Encode jitter
	if err := enc.Encode(int64(t.Jitter)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder for deserialization.
func (t *IntervalTrigger) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var intervalNs int64
	if err := dec.Decode(&intervalNs); err != nil {
		return err
	}
	t.Interval = time.Duration(intervalNs)

	var startSec int64
	var startNsec int
	if err := dec.Decode(&startSec); err != nil {
		return err
	}
	if err := dec.Decode(&startNsec); err != nil {
		return err
	}

	var hasEndDate bool
	if err := dec.Decode(&hasEndDate); err != nil {
		return err
	}
	if hasEndDate {
		var endSec int64
		var endNsec int
		if err := dec.Decode(&endSec); err != nil {
			return err
		}
		if err := dec.Decode(&endNsec); err != nil {
			return err
		}
		endDate := time.Unix(endSec, int64(endNsec))
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

	var jitterNs int64
	if err := dec.Decode(&jitterNs); err != nil {
		return err
	}
	t.Jitter = time.Duration(jitterNs)

	t.StartDate = time.Unix(startSec, int64(startNsec)).In(t.Location)
	if t.EndDate != nil {
		endDate := t.EndDate.In(t.Location)
		t.EndDate = &endDate
	}

	return nil
}

func init() {
	gob.Register(&IntervalTrigger{})
}
