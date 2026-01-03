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

// GoroutinePoolExecutor executes jobs using a goroutine worker pool.
type GoroutinePoolExecutor struct {
	mu         sync.Mutex
	maxWorkers int
	instances  map[string]int // jobID -> running instance count

	scheduler     interface{}
	alias         string
	eventDispatch func(event.Event)

	// Worker pool
	jobQueue     chan *jobSubmission
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	wg           sync.WaitGroup
}

type jobSubmission struct {
	job          *job.Job
	runTimes     []time.Time
	jobStoreAlias string
}

// NewGoroutinePoolExecutor creates a new goroutine pool executor.
func NewGoroutinePoolExecutor(maxWorkers int) *GoroutinePoolExecutor {
	if maxWorkers <= 0 {
		maxWorkers = 10
	}
	return &GoroutinePoolExecutor{
		maxWorkers: maxWorkers,
		instances:  make(map[string]int),
		jobQueue:   make(chan *jobSubmission, maxWorkers*10),
		shutdownCh: make(chan struct{}),
	}
}

// Start initializes and starts the worker pool.
func (e *GoroutinePoolExecutor) Start(scheduler interface{}, alias string) error {
	e.scheduler = scheduler
	e.alias = alias

	// Check if scheduler implements EventDispatcher
	if dispatcher, ok := scheduler.(EventDispatcher); ok {
		e.eventDispatch = dispatcher.DispatchEvent
	}

	// Start worker goroutines
	for i := 0; i < e.maxWorkers; i++ {
		e.wg.Add(1)
		go e.worker()
	}

	return nil
}

// worker processes jobs from the queue.
func (e *GoroutinePoolExecutor) worker() {
	defer e.wg.Done()

	for {
		select {
		case <-e.shutdownCh:
			return
		case submission := <-e.jobQueue:
			if submission != nil {
				e.runJob(submission)
			}
		}
	}
}

// SubmitJob submits a job for execution.
func (e *GoroutinePoolExecutor) SubmitJob(j *job.Job, runTimes []time.Time) error {
	e.mu.Lock()

	// Check max instances
	if e.instances[j.ID] >= j.MaxInstances {
		e.mu.Unlock()
		return &MaxInstancesReachedError{Job: j}
	}

	e.instances[j.ID]++
	e.mu.Unlock()

	// Submit to worker pool
	submission := &jobSubmission{
		job:           j,
		runTimes:      runTimes,
		jobStoreAlias: j.GetJobStoreAlias(),
	}

	select {
	case e.jobQueue <- submission:
		return nil
	case <-e.shutdownCh:
		// Decrement instance count since we didn't actually submit
		e.mu.Lock()
		e.instances[j.ID]--
		if e.instances[j.ID] == 0 {
			delete(e.instances, j.ID)
		}
		e.mu.Unlock()
		return fmt.Errorf("executor is shutting down")
	}
}

// runJob executes the job and dispatches events.
func (e *GoroutinePoolExecutor) runJob(submission *jobSubmission) {
	j := submission.job
	jobStoreAlias := submission.jobStoreAlias

	defer func() {
		e.mu.Lock()
		e.instances[j.ID]--
		if e.instances[j.ID] == 0 {
			delete(e.instances, j.ID)
		}
		e.mu.Unlock()
	}()

	for _, runTime := range submission.runTimes {
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
func (e *GoroutinePoolExecutor) executeFunc(j *job.Job, runTime time.Time) *RunResult {
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
func (e *GoroutinePoolExecutor) dispatchEvent(evt event.Event) {
	if e.eventDispatch != nil {
		e.eventDispatch(evt)
	}
}

// Shutdown stops the executor.
func (e *GoroutinePoolExecutor) Shutdown(wait bool) error {
	e.shutdownOnce.Do(func() {
		close(e.shutdownCh)
	})

	if wait {
		e.wg.Wait()
	}

	return nil
}
