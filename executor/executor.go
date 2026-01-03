// Package executor provides job execution implementations.
package executor

import (
	"fmt"
	"time"

	"github.com/georgepadayatti/goscheduler/event"
	"github.com/georgepadayatti/goscheduler/job"
)

// Executor is the interface for job executors.
type Executor interface {
	// Start initializes the executor.
	Start(scheduler interface{}, alias string) error

	// Shutdown stops the executor.
	// If wait is true, waits for running jobs to complete.
	Shutdown(wait bool) error

	// SubmitJob submits a job for execution.
	SubmitJob(j *job.Job, runTimes []time.Time) error
}

// EventDispatcher is implemented by the scheduler to dispatch events.
type EventDispatcher interface {
	DispatchEvent(e event.Event)
}

// MaxInstancesReachedError is returned when a job has too many concurrent instances.
type MaxInstancesReachedError struct {
	Job *job.Job
}

func (e *MaxInstancesReachedError) Error() string {
	return fmt.Sprintf("job %q has reached maximum instances (%d)", e.Job.ID, e.Job.MaxInstances)
}

// RunResult contains the result of executing a job.
type RunResult struct {
	JobID       string
	RunTime     time.Time
	ReturnValue interface{}
	Error       error
	Traceback   string
}
