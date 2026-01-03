package trigger

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"strings"
	"time"
)

// AndTrigger returns the earliest fire time that all triggers agree on.
// It's finished when any trigger finishes.
type AndTrigger struct {
	Triggers []Trigger
	Jitter   time.Duration
}

// NewAndTrigger creates a new AndTrigger.
func NewAndTrigger(triggers []Trigger, jitter time.Duration) *AndTrigger {
	return &AndTrigger{
		Triggers: triggers,
		Jitter:   jitter,
	}
}

// GetNextFireTime finds the next time all triggers agree on.
func (t *AndTrigger) GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time {
	currentNow := now

	for iterations := 0; iterations < 1000; iterations++ { // Prevent infinite loop
		var fireTimes []*time.Time
		var minTime, maxTime *time.Time

		for _, trig := range t.Triggers {
			ft := trig.GetNextFireTime(previousFireTime, currentNow)
			if ft == nil {
				return nil // One trigger is finished
			}
			fireTimes = append(fireTimes, ft)

			if minTime == nil || ft.Before(*minTime) {
				minTime = ft
			}
			if maxTime == nil || ft.After(*maxTime) {
				maxTime = ft
			}
		}

		// All triggers agree on the same time
		if minTime.Equal(*maxTime) {
			return ApplyJitter(minTime, t.Jitter, now)
		}

		// Try again with the max time as now
		currentNow = *maxTime
	}

	return nil // Couldn't find agreement
}

// String returns a string representation.
func (t *AndTrigger) String() string {
	var parts []string
	for _, trig := range t.Triggers {
		parts = append(parts, trig.String())
	}
	return fmt.Sprintf("and[%s]", strings.Join(parts, ", "))
}

// GobEncode implements gob.GobEncoder for serialization.
func (t *AndTrigger) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	// Encode number of triggers
	if err := enc.Encode(len(t.Triggers)); err != nil {
		return nil, err
	}

	// Encode each trigger
	for _, trig := range t.Triggers {
		if err := enc.Encode(&trig); err != nil {
			return nil, err
		}
	}

	// Encode jitter
	if err := enc.Encode(int64(t.Jitter)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder for deserialization.
func (t *AndTrigger) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var count int
	if err := dec.Decode(&count); err != nil {
		return err
	}

	t.Triggers = make([]Trigger, count)
	for i := 0; i < count; i++ {
		var trig Trigger
		if err := dec.Decode(&trig); err != nil {
			return err
		}
		t.Triggers[i] = trig
	}

	var jitterNs int64
	if err := dec.Decode(&jitterNs); err != nil {
		return err
	}
	t.Jitter = time.Duration(jitterNs)

	return nil
}

// OrTrigger returns the earliest fire time from any trigger.
// It's finished when all triggers finish.
type OrTrigger struct {
	Triggers []Trigger
	Jitter   time.Duration
}

// NewOrTrigger creates a new OrTrigger.
func NewOrTrigger(triggers []Trigger, jitter time.Duration) *OrTrigger {
	return &OrTrigger{
		Triggers: triggers,
		Jitter:   jitter,
	}
}

// GetNextFireTime returns the earliest fire time from any trigger.
func (t *OrTrigger) GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time {
	var earliest *time.Time

	for _, trig := range t.Triggers {
		ft := trig.GetNextFireTime(previousFireTime, now)
		if ft != nil && (earliest == nil || ft.Before(*earliest)) {
			earliest = ft
		}
	}

	if earliest != nil {
		return ApplyJitter(earliest, t.Jitter, now)
	}
	return nil
}

// String returns a string representation.
func (t *OrTrigger) String() string {
	var parts []string
	for _, trig := range t.Triggers {
		parts = append(parts, trig.String())
	}
	return fmt.Sprintf("or[%s]", strings.Join(parts, ", "))
}

// GobEncode implements gob.GobEncoder for serialization.
func (t *OrTrigger) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	if err := enc.Encode(len(t.Triggers)); err != nil {
		return nil, err
	}

	for _, trig := range t.Triggers {
		if err := enc.Encode(&trig); err != nil {
			return nil, err
		}
	}

	if err := enc.Encode(int64(t.Jitter)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder for deserialization.
func (t *OrTrigger) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var count int
	if err := dec.Decode(&count); err != nil {
		return err
	}

	t.Triggers = make([]Trigger, count)
	for i := 0; i < count; i++ {
		var trig Trigger
		if err := dec.Decode(&trig); err != nil {
			return err
		}
		t.Triggers[i] = trig
	}

	var jitterNs int64
	if err := dec.Decode(&jitterNs); err != nil {
		return err
	}
	t.Jitter = time.Duration(jitterNs)

	return nil
}

func init() {
	gob.Register(&AndTrigger{})
	gob.Register(&OrTrigger{})
}
