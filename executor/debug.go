package executor

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	"github.com/georgejpadayatti/goscheduler/event"
	"github.com/georgejpadayatti/goscheduler/job"
)

// DebugExecutor executes jobs synchronously in the calling goroutine.
// This is useful for testing and debugging purposes where you want
// predictable, sequential job execution.
type DebugExecutor struct {
	mu        sync.Mutex
	instances map[string]int // jobID -> running instance count

	scheduler     any
	alias         string
	eventDispatch func(event.Event)
}

// NewDebugExecutor creates a new debug executor.
func NewDebugExecutor() *DebugExecutor {
	return &DebugExecutor{
		instances: make(map[string]int),
	}
}

// Start initializes the debug executor.
func (e *DebugExecutor) Start(scheduler any, alias string) error {
	e.scheduler = scheduler
	e.alias = alias

	// Check if scheduler implements EventDispatcher
	if dispatcher, ok := scheduler.(EventDispatcher); ok {
		e.eventDispatch = dispatcher.DispatchEvent
	}

	return nil
}

// SubmitJob executes the job synchronously.
func (e *DebugExecutor) SubmitJob(j *job.Job, runTimes []time.Time) error {
	e.mu.Lock()

	// Check max instances
	if e.instances[j.ID] >= j.MaxInstances {
		e.mu.Unlock()
		return &MaxInstancesReachedError{Job: j}
	}

	e.instances[j.ID]++
	e.mu.Unlock()

	// Execute synchronously
	e.runJob(j, runTimes)

	return nil
}

// runJob executes the job and dispatches events.
func (e *DebugExecutor) runJob(j *job.Job, runTimes []time.Time) {
	jobStoreAlias := j.GetJobStoreAlias()

	defer func() {
		e.mu.Lock()
		e.instances[j.ID]--
		if e.instances[j.ID] == 0 {
			delete(e.instances, j.ID)
		}
		e.mu.Unlock()
	}()

	for _, runTime := range runTimes {
		// Check misfire grace time
		if j.MisfireGraceTime != nil {
			elapsed := time.Since(runTime)
			if elapsed > *j.MisfireGraceTime {
				// Job missed its run time
				e.dispatchEvent(event.NewJobMissedEvent(j.ID, jobStoreAlias, runTime))
				continue
			}
		}

		// Execute the job function
		result := e.executeFunc(j, runTime)

		if result.Error != nil {
			e.dispatchEvent(event.NewJobErrorEvent(
				j.ID, jobStoreAlias, runTime, result.Error, result.Traceback,
			))
		} else {
			e.dispatchEvent(event.NewJobExecutedEvent(
				j.ID, jobStoreAlias, runTime, result.ReturnValue,
			))
		}
	}
}

// executeFunc calls the job function using reflection.
func (e *DebugExecutor) executeFunc(j *job.Job, runTime time.Time) *RunResult {
	result := &RunResult{
		JobID:   j.ID,
		RunTime: runTime,
	}

	// Recover from panics
	defer func() {
		if r := recover(); r != nil {
			result.Error = fmt.Errorf("panic: %v", r)
			result.Traceback = string(debug.Stack())
		}
	}()

	fn := j.Func
	if fn == nil {
		result.Error = fmt.Errorf("job function is nil")
		return result
	}

	fnVal := reflect.ValueOf(fn)
	if fnVal.Kind() != reflect.Func {
		result.Error = fmt.Errorf("job function is not callable")
		return result
	}

	// Prepare arguments
	var args []reflect.Value
	for _, arg := range j.Args {
		args = append(args, reflect.ValueOf(arg))
	}

	// Call the function
	results := fnVal.Call(args)

	// Handle return values
	if len(results) > 0 {
		lastResult := results[len(results)-1]
		// Check if the last return value is an error
		if lastResult.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !lastResult.IsNil() {
				result.Error = lastResult.Interface().(error)
			}
			// If there are other return values, capture them
			if len(results) > 1 {
				result.ReturnValue = results[0].Interface()
			}
		} else if len(results) == 1 {
			result.ReturnValue = results[0].Interface()
		}
	}

	return result
}

// dispatchEvent sends an event to the scheduler.
func (e *DebugExecutor) dispatchEvent(evt event.Event) {
	if e.eventDispatch != nil {
		e.eventDispatch(evt)
	}
}

// Shutdown stops the executor.
func (e *DebugExecutor) Shutdown(wait bool) error {
	// Nothing to do for debug executor
	return nil
}
