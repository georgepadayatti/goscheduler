// Package trigger provides trigger implementations for job scheduling.
package trigger

import (
	"math/rand"
	"time"
)

// Trigger defines the interface that all triggers must implement.
// A trigger determines when a job should be executed.
type Trigger interface {
	// GetNextFireTime returns the next datetime to fire on.
	// Returns nil if no such datetime can be calculated (schedule finished).
	//
	// previousFireTime: the previous time the trigger was fired (nil for first calculation)
	// now: current datetime
	GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time

	// String returns a human-readable representation of the trigger.
	String() string
}

// Jitterable is an optional interface for triggers that support jitter.
type Jitterable interface {
	Trigger
	SetJitter(d time.Duration)
	GetJitter() time.Duration
}

// ApplyJitter adds a random jitter to the next fire time.
// If nextFireTime is nil or jitter is 0, returns nextFireTime unchanged.
func ApplyJitter(nextFireTime *time.Time, jitter time.Duration, now time.Time) *time.Time {
	if nextFireTime == nil || jitter == 0 {
		return nextFireTime
	}

	// Add random jitter between 0 and jitter duration
	jitterNs := rand.Int63n(int64(jitter))
	result := nextFireTime.Add(time.Duration(jitterNs))
	return &result
}

// TimePtr is a helper to create a pointer to a time.Time value.
func TimePtr(t time.Time) *time.Time {
	return &t
}

// DurationPtr is a helper to create a pointer to a time.Duration value.
func DurationPtr(d time.Duration) *time.Duration {
	return &d
}
