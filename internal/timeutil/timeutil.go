// Package timeutil provides time-related utility functions for the scheduler.
package timeutil

import (
	"math"
	"time"
)

// Ceil rounds up a time to the nearest second boundary.
// This is used to ensure job run times start at clean second boundaries.
func Ceil(t time.Time) time.Time {
	if t.Nanosecond() == 0 {
		return t
	}
	return t.Truncate(time.Second).Add(time.Second)
}

// ToTimestamp converts a time.Time to a float64 UTC timestamp.
// Returns nil if the input time pointer is nil.
func ToTimestamp(t *time.Time) *float64 {
	if t == nil {
		return nil
	}
	ts := float64(t.UTC().Unix()) + float64(t.Nanosecond())/1e9
	return &ts
}

// FromTimestamp converts a float64 UTC timestamp to time.Time.
// Returns nil if the input timestamp pointer is nil.
func FromTimestamp(ts *float64) *time.Time {
	if ts == nil {
		return nil
	}
	sec := int64(*ts)
	nsec := int64((*ts - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec).UTC()
	return &t
}

// InfinityTimestamp returns positive infinity as a float64.
// Used for sorting paused jobs (nil next_run_time) to the end.
func InfinityTimestamp() float64 {
	return math.Inf(1)
}

// TimestampLess compares two timestamps for sorting.
// nil timestamps (paused jobs) are considered greater than any actual time.
func TimestampLess(a, b *float64) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false // nil is "greater" (sorts last)
	}
	if b == nil {
		return true // anything is less than nil
	}
	return *a < *b
}

// TimePtr returns a pointer to the given time.
func TimePtr(t time.Time) *time.Time {
	return &t
}

// DurationPtr returns a pointer to the given duration.
func DurationPtr(d time.Duration) *time.Duration {
	return &d
}

// MinTime returns the earlier of two times.
func MinTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// MaxTime returns the later of two times.
func MaxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// DaysInMonth returns the number of days in the given month of the given year.
func DaysInMonth(year int, month time.Month) int {
	// Get the first day of the next month, then subtract one day
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// IsLeapYear returns true if the given year is a leap year.
func IsLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
