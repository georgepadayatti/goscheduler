// Package event provides the event system for scheduler notifications.
package event

import "time"

// Code represents event type codes using bitmask values.
// This allows efficient filtering of events using bitwise operations.
type Code uint32

const (
	// Scheduler lifecycle events
	SchedulerStarted  Code = 1 << iota // Scheduler has started
	SchedulerShutdown                  // Scheduler has shut down
	SchedulerPaused                    // Scheduler has been paused
	SchedulerResumed                   // Scheduler has been resumed

	// Component events
	ExecutorAdded   // An executor was added
	ExecutorRemoved // An executor was removed
	JobStoreAdded   // A job store was added
	JobStoreRemoved // A job store was removed

	// Job lifecycle events
	AllJobsRemoved  // All jobs were removed from a store
	JobAdded        // A job was added
	JobRemoved      // A job was removed
	JobModified     // A job was modified
	JobExecuted     // A job was executed successfully
	JobError        // A job execution resulted in an error
	JobMissed       // A job missed its scheduled run time
	JobSubmitted    // A job was submitted to an executor
	JobMaxInstances // A job couldn't run due to max instances limit

	// Convenience masks
	All Code = (1 << 17) - 1 // All events
)

// Event is the base interface for all scheduler events.
type Event interface {
	EventCode() Code
}

// SchedulerEvent represents events related to the scheduler itself.
type SchedulerEvent struct {
	code  Code
	Alias string // For executor/jobstore add/remove events
}

// EventCode returns the event code.
func (e *SchedulerEvent) EventCode() Code { return e.code }

// NewSchedulerEvent creates a new scheduler event.
func NewSchedulerEvent(code Code, alias string) *SchedulerEvent {
	return &SchedulerEvent{code: code, Alias: alias}
}

// NewSchedulerStartedEvent creates a scheduler started event.
func NewSchedulerStartedEvent() *SchedulerEvent {
	return &SchedulerEvent{code: SchedulerStarted}
}

// NewSchedulerShutdownEvent creates a scheduler shutdown event.
func NewSchedulerShutdownEvent() *SchedulerEvent {
	return &SchedulerEvent{code: SchedulerShutdown}
}

// NewSchedulerPausedEvent creates a scheduler paused event.
func NewSchedulerPausedEvent() *SchedulerEvent {
	return &SchedulerEvent{code: SchedulerPaused}
}

// NewSchedulerResumedEvent creates a scheduler resumed event.
func NewSchedulerResumedEvent() *SchedulerEvent {
	return &SchedulerEvent{code: SchedulerResumed}
}

// JobEvent represents events related to a specific job.
type JobEvent struct {
	code     Code
	JobID    string
	JobStore string
}

// EventCode returns the event code.
func (e *JobEvent) EventCode() Code { return e.code }

// NewJobEvent creates a new job event.
func NewJobEvent(code Code, jobID, jobStore string) *JobEvent {
	return &JobEvent{code: code, JobID: jobID, JobStore: jobStore}
}

// NewJobAddedEvent creates a job added event.
func NewJobAddedEvent(jobID, jobStore string) *JobEvent {
	return NewJobEvent(JobAdded, jobID, jobStore)
}

// NewJobRemovedEvent creates a job removed event.
func NewJobRemovedEvent(jobID, jobStore string) *JobEvent {
	return NewJobEvent(JobRemoved, jobID, jobStore)
}

// NewJobModifiedEvent creates a job modified event.
func NewJobModifiedEvent(jobID, jobStore string) *JobEvent {
	return NewJobEvent(JobModified, jobID, jobStore)
}

// JobSubmissionEvent represents a job submission to an executor.
type JobSubmissionEvent struct {
	JobEvent
	ScheduledRunTimes []time.Time
}

// NewJobSubmittedEvent creates a job submitted event.
func NewJobSubmittedEvent(jobID, jobStore string, runTimes []time.Time) *JobSubmissionEvent {
	return &JobSubmissionEvent{
		JobEvent:          JobEvent{code: JobSubmitted, JobID: jobID, JobStore: jobStore},
		ScheduledRunTimes: runTimes,
	}
}

// NewJobMaxInstancesEvent creates a max instances reached event.
func NewJobMaxInstancesEvent(jobID, jobStore string, runTimes []time.Time) *JobSubmissionEvent {
	return &JobSubmissionEvent{
		JobEvent:          JobEvent{code: JobMaxInstances, JobID: jobID, JobStore: jobStore},
		ScheduledRunTimes: runTimes,
	}
}

// JobExecutionEvent represents the result of a job execution.
type JobExecutionEvent struct {
	JobEvent
	ScheduledRunTime time.Time
	ReturnValue      interface{}
	Exception        error
	Traceback        string
}

// NewJobExecutedEvent creates a job executed successfully event.
func NewJobExecutedEvent(jobID, jobStore string, runTime time.Time, retval interface{}) *JobExecutionEvent {
	return &JobExecutionEvent{
		JobEvent:         JobEvent{code: JobExecuted, JobID: jobID, JobStore: jobStore},
		ScheduledRunTime: runTime,
		ReturnValue:      retval,
	}
}

// NewJobErrorEvent creates a job error event.
func NewJobErrorEvent(jobID, jobStore string, runTime time.Time, err error, traceback string) *JobExecutionEvent {
	return &JobExecutionEvent{
		JobEvent:         JobEvent{code: JobError, JobID: jobID, JobStore: jobStore},
		ScheduledRunTime: runTime,
		Exception:        err,
		Traceback:        traceback,
	}
}

// NewJobMissedEvent creates a job missed event.
func NewJobMissedEvent(jobID, jobStore string, runTime time.Time) *JobExecutionEvent {
	return &JobExecutionEvent{
		JobEvent:         JobEvent{code: JobMissed, JobID: jobID, JobStore: jobStore},
		ScheduledRunTime: runTime,
	}
}
