package trigger

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"time"
)

// DateTrigger triggers once at a specific datetime.
// If RunDate is not set, it defaults to the current time.
type DateTrigger struct {
	RunDate  time.Time
	Location *time.Location
}

// DateOption configures a DateTrigger.
type DateOption func(*DateTrigger)

// WithDateLocation sets the timezone for the trigger.
func WithDateLocation(loc *time.Location) DateOption {
	return func(t *DateTrigger) {
		t.Location = loc
	}
}

// NewDateTrigger creates a new DateTrigger.
// If runDate is zero, the current time is used.
func NewDateTrigger(runDate time.Time, opts ...DateOption) *DateTrigger {
	t := &DateTrigger{
		Location: time.Local,
	}

	// Apply options
	for _, opt := range opts {
		opt(t)
	}

	if runDate.IsZero() {
		t.RunDate = time.Now().In(t.Location)
	} else {
		t.RunDate = runDate.In(t.Location)
	}

	return t
}

// GetNextFireTime returns the run date if this is the first call,
// or nil for subsequent calls (one-shot trigger).
func (t *DateTrigger) GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time {
	if previousFireTime == nil {
		return &t.RunDate
	}
	return nil
}

// String returns a string representation of the trigger.
func (t *DateTrigger) String() string {
	return fmt.Sprintf("date[%s]", t.RunDate.Format(time.RFC3339))
}

// GobEncode implements gob.GobEncoder for serialization.
func (t *DateTrigger) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	// Encode run date as Unix timestamp and timezone name
	if err := enc.Encode(t.RunDate.Unix()); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.RunDate.Nanosecond()); err != nil {
		return nil, err
	}
	if err := enc.Encode(t.Location.String()); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder for deserialization.
func (t *DateTrigger) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var sec int64
	var nsec int
	var locName string

	if err := dec.Decode(&sec); err != nil {
		return err
	}
	if err := dec.Decode(&nsec); err != nil {
		return err
	}
	if err := dec.Decode(&locName); err != nil {
		return err
	}

	loc, err := time.LoadLocation(locName)
	if err != nil {
		loc = time.UTC
	}

	t.Location = loc
	t.RunDate = time.Unix(sec, int64(nsec)).In(loc)

	return nil
}

func init() {
	gob.Register(&DateTrigger{})
}
