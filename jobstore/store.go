// Package jobstore provides job persistence implementations.
package jobstore

import (
	"fmt"
	"reflect"
	"time"

	"github.com/georgejpadayatti/goscheduler/job"
)

// JobStore is the interface for job persistence backends.
type JobStore interface {
	// Start initializes the job store.
	Start(scheduler interface{}, alias string) error

	// Shutdown releases any resources held by the store.
	Shutdown() error

	// LookupJob returns a job by ID, or nil if not found.
	LookupJob(jobID string) (*job.Job, error)

	// GetDueJobs returns jobs with next_run_time <= now, sorted ascending.
	GetDueJobs(now time.Time) ([]*job.Job, error)

	// GetNextRunTime returns the earliest next_run_time, or nil if no active jobs.
	GetNextRunTime() *time.Time

	// GetAllJobs returns all jobs sorted by next_run_time (paused jobs last).
	GetAllJobs() ([]*job.Job, error)

	// AddJob adds a job to the store.
	AddJob(j *job.Job) error

	// UpdateJob updates an existing job.
	UpdateJob(j *job.Job) error

	// RemoveJob removes a job by ID.
	RemoveJob(jobID string) error

	// RemoveAllJobs removes all jobs from the store.
	RemoveAllJobs() error
}

// JobLookupError is returned when a job is not found.
type JobLookupError struct {
	JobID string
}

func (e *JobLookupError) Error() string {
	return fmt.Sprintf("no job with id %q was found", e.JobID)
}

// ConflictingIDError is returned when a job ID already exists.
type ConflictingIDError struct {
	JobID string
}

func (e *ConflictingIDError) Error() string {
	return fmt.Sprintf("job id %q conflicts with an existing job", e.JobID)
}

// TransientJobError is returned when a non-serializable job is added to a persistent store.
type TransientJobError struct {
	JobID string
}

func (e *TransientJobError) Error() string {
	return fmt.Sprintf("job %q cannot be added because it has no function reference", e.JobID)
}

func isNilValue(value interface{}) bool {
	if value == nil {
		return true
	}

	val := reflect.ValueOf(value)
	switch val.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr, reflect.Interface, reflect.Slice:
		return val.IsNil()
	default:
		return false
	}
}
