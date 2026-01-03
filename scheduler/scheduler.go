// Package scheduler provides the core scheduling functionality.
package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/georgepadayatti/goscheduler/event"
	"github.com/georgepadayatti/goscheduler/executor"
	"github.com/georgepadayatti/goscheduler/job"
	"github.com/georgepadayatti/goscheduler/jobstore"
	"github.com/georgepadayatti/goscheduler/trigger"
)

// State represents the scheduler's running state.
type State int

const (
	StateStopped State = iota
	StateRunning
	StatePaused
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateRunning:
		return "running"
	case StatePaused:
		return "paused"
	default:
		return "unknown"
	}
}

// Scheduler is the interface for job schedulers.
type Scheduler interface {
	// Start begins job processing.
	Start(ctx context.Context) error

	// Shutdown stops the scheduler gracefully.
	Shutdown(wait bool) error

	// Pause temporarily suspends job processing.
	Pause() error

	// Resume resumes job processing.
	Resume() error

	// State returns the current scheduler state.
	State() State

	// AddJob schedules a new job.
	AddJob(fn interface{}, trig trigger.Trigger, opts ...job.Option) (*job.Job, error)

	// ModifyJob updates job properties.
	ModifyJob(jobID string, opts ...job.Option) (*job.Job, error)

	// RescheduleJob changes a job's trigger.
	RescheduleJob(jobID string, trig trigger.Trigger) (*job.Job, error)

	// PauseJob suspends a job's execution.
	PauseJob(jobID string) (*job.Job, error)

	// ResumeJob resumes a paused job.
	ResumeJob(jobID string) (*job.Job, error)

	// RemoveJob unschedules a job.
	RemoveJob(jobID string) error

	// RemoveAllJobs removes all jobs from all job stores.
	RemoveAllJobs() error

	// GetJob retrieves a job by ID.
	GetJob(jobID string) (*job.Job, error)

	// GetJobs returns all scheduled jobs.
	GetJobs() ([]*job.Job, error)

	// AddListener registers an event listener.
	AddListener(callback event.Callback, mask event.Code)

	// RemoveListener unregisters an event listener.
	RemoveListener(callback event.Callback)

	// AddExecutor adds an executor with the given alias.
	AddExecutor(exec executor.Executor, alias string) error

	// AddJobStore adds a job store with the given alias.
	AddJobStore(store jobstore.JobStore, alias string) error
}

// Common errors
var (
	ErrSchedulerAlreadyRunning = errors.New("scheduler is already running")
	ErrSchedulerNotRunning     = errors.New("scheduler is not running")
	ErrNoSuchExecutor          = errors.New("no executor with the specified alias")
	ErrNoSuchJobStore          = errors.New("no job store with the specified alias")
)

// Config holds scheduler configuration.
type Config struct {
	// Timezone for date/time calculations (default: time.Local)
	Timezone *time.Location

	// Retry interval on job store errors (default: 10s)
	JobStoreRetryInterval time.Duration

	// Default job settings
	DefaultMisfireGraceTime time.Duration
	DefaultCoalesce         bool
	DefaultMaxInstances     int
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Timezone:                time.Local,
		JobStoreRetryInterval:   10 * time.Second,
		DefaultMisfireGraceTime: 0,
		DefaultCoalesce:         true,
		DefaultMaxInstances:     1,
	}
}
