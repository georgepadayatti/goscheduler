package scheduler

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/georgepadayatti/goscheduler/event"
	"github.com/georgepadayatti/goscheduler/executor"
	"github.com/georgepadayatti/goscheduler/job"
	"github.com/georgepadayatti/goscheduler/jobstore"
	"github.com/georgepadayatti/goscheduler/trigger"
)

// BaseScheduler provides the core scheduling functionality.
type BaseScheduler struct {
	mu    sync.RWMutex
	state State

	timezone              *time.Location
	jobStoreRetryInterval time.Duration

	executors   map[string]executor.Executor
	executorsMu sync.RWMutex

	jobstores   map[string]jobstore.JobStore
	jobstoresMu sync.RWMutex

	listeners *event.ListenerManager

	pendingJobs   []*pendingJob
	pendingJobsMu sync.Mutex

	submitRetry   map[string]time.Time
	submitRetryMu sync.Mutex

	// Job defaults
	defaultMisfireGrace time.Duration
	defaultCoalesce     bool
	defaultMaxInstances int

	// Wakeup mechanism
	wakeupCh chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
}

type pendingJob struct {
	job             *job.Job
	jobstoreAlias   string
	replaceExisting bool
}

// NewBaseScheduler creates a new base scheduler.
func NewBaseScheduler(cfg Config) *BaseScheduler {
	if cfg.Timezone == nil {
		cfg.Timezone = time.Local
	}
	if cfg.JobStoreRetryInterval == 0 {
		cfg.JobStoreRetryInterval = 10 * time.Second
	}
	if cfg.DefaultMaxInstances == 0 {
		cfg.DefaultMaxInstances = 1
	}

	return &BaseScheduler{
		state:                 StateStopped,
		timezone:              cfg.Timezone,
		jobStoreRetryInterval: cfg.JobStoreRetryInterval,
		executors:             make(map[string]executor.Executor),
		jobstores:             make(map[string]jobstore.JobStore),
		listeners:             event.NewListenerManager(),
		defaultMisfireGrace:   cfg.DefaultMisfireGraceTime,
		defaultCoalesce:       cfg.DefaultCoalesce,
		defaultMaxInstances:   cfg.DefaultMaxInstances,
		submitRetry:           make(map[string]time.Time),
		wakeupCh:              make(chan struct{}, 1),
	}
}

// State returns the current scheduler state.
func (s *BaseScheduler) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// AddExecutor adds an executor with the given alias.
func (s *BaseScheduler) AddExecutor(exec executor.Executor, alias string) error {
	s.executorsMu.Lock()
	defer s.executorsMu.Unlock()

	if _, exists := s.executors[alias]; exists {
		return fmt.Errorf("executor alias %q already exists", alias)
	}

	s.executors[alias] = exec

	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()

	if state != StateStopped {
		if err := exec.Start(s, alias); err != nil {
			delete(s.executors, alias)
			return err
		}
	}

	s.listeners.Dispatch(event.NewSchedulerEvent(event.ExecutorAdded, alias))
	return nil
}

// AddJobStore adds a job store with the given alias.
func (s *BaseScheduler) AddJobStore(store jobstore.JobStore, alias string) error {
	s.jobstoresMu.Lock()
	defer s.jobstoresMu.Unlock()

	if _, exists := s.jobstores[alias]; exists {
		return fmt.Errorf("job store alias %q already exists", alias)
	}

	s.jobstores[alias] = store

	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()

	if state != StateStopped {
		if err := store.Start(s, alias); err != nil {
			delete(s.jobstores, alias)
			return err
		}
	}

	s.listeners.Dispatch(event.NewSchedulerEvent(event.JobStoreAdded, alias))
	return nil
}

// AddListener registers an event listener.
func (s *BaseScheduler) AddListener(callback event.Callback, mask event.Code) {
	s.listeners.AddListener(callback, mask)
}

// RemoveListener unregisters an event listener.
func (s *BaseScheduler) RemoveListener(callback event.Callback) {
	s.listeners.RemoveListener(callback)
}

// DispatchEvent dispatches an event to all listeners.
func (s *BaseScheduler) DispatchEvent(e event.Event) {
	s.listeners.Dispatch(e)
}

// AddJob schedules a new job.
func (s *BaseScheduler) AddJob(fn interface{}, trig trigger.Trigger, opts ...job.Option) (*job.Job, error) {
	// Create the job
	j, err := job.New(fn, trig, opts...)
	if err != nil {
		return nil, err
	}

	// Apply defaults
	if j.MisfireGraceTime == nil && s.defaultMisfireGrace > 0 {
		j.MisfireGraceTime = &s.defaultMisfireGrace
	}
	if j.MaxInstances == 0 {
		j.MaxInstances = s.defaultMaxInstances
	}

	// Calculate initial next run time
	now := time.Now().In(s.timezone)
	j.NextRunTime = trig.GetNextFireTime(nil, now)

	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()

	if state == StateStopped {
		// Queue for later
		s.pendingJobsMu.Lock()
		s.pendingJobs = append(s.pendingJobs, &pendingJob{
			job:           j,
			jobstoreAlias: j.JobStore,
		})
		s.pendingJobsMu.Unlock()
	} else {
		// Add to job store immediately
		if err := s.addJobToStore(j, j.JobStore); err != nil {
			return nil, err
		}
	}

	j.SetScheduler(s)
	s.listeners.Dispatch(event.NewJobAddedEvent(j.ID, j.JobStore))
	if state != StateStopped {
		s.wakeup()
	}

	return j, nil
}

// addJobToStore adds a job to the specified job store.
func (s *BaseScheduler) addJobToStore(j *job.Job, alias string) error {
	s.jobstoresMu.RLock()
	store, exists := s.jobstores[alias]
	s.jobstoresMu.RUnlock()

	if !exists {
		return fmt.Errorf("no job store with alias %q", alias)
	}

	if err := store.AddJob(j); err != nil {
		return err
	}

	j.SetJobStoreAlias(alias)
	return nil
}

// ModifyJob updates job properties.
func (s *BaseScheduler) ModifyJob(jobID string, opts ...job.Option) (*job.Job, error) {
	j, alias, err := s.lookupJob(jobID)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, &jobstore.JobLookupError{JobID: jobID}
	}

	if err := j.Modify(opts...); err != nil {
		return nil, err
	}

	s.jobstoresMu.RLock()
	store := s.jobstores[alias]
	s.jobstoresMu.RUnlock()

	if err := store.UpdateJob(j); err != nil {
		return nil, err
	}

	s.listeners.Dispatch(event.NewJobModifiedEvent(j.ID, alias))
	s.wakeup()

	return j, nil
}

// RescheduleJob changes a job's trigger.
func (s *BaseScheduler) RescheduleJob(jobID string, trig trigger.Trigger) (*job.Job, error) {
	j, alias, err := s.lookupJob(jobID)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, &jobstore.JobLookupError{JobID: jobID}
	}

	now := time.Now().In(s.timezone)
	j.Trigger = trig
	j.NextRunTime = trig.GetNextFireTime(nil, now)

	s.jobstoresMu.RLock()
	store := s.jobstores[alias]
	s.jobstoresMu.RUnlock()

	if err := store.UpdateJob(j); err != nil {
		return nil, err
	}

	s.listeners.Dispatch(event.NewJobModifiedEvent(j.ID, alias))
	s.wakeup()

	return j, nil
}

// PauseJob suspends a job's execution.
func (s *BaseScheduler) PauseJob(jobID string) (*job.Job, error) {
	j, alias, err := s.lookupJob(jobID)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, &jobstore.JobLookupError{JobID: jobID}
	}

	j.Pause()

	s.jobstoresMu.RLock()
	store := s.jobstores[alias]
	s.jobstoresMu.RUnlock()

	if err := store.UpdateJob(j); err != nil {
		return nil, err
	}

	s.listeners.Dispatch(event.NewJobModifiedEvent(j.ID, alias))
	return j, nil
}

// ResumeJob resumes a paused job.
func (s *BaseScheduler) ResumeJob(jobID string) (*job.Job, error) {
	j, alias, err := s.lookupJob(jobID)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, &jobstore.JobLookupError{JobID: jobID}
	}

	now := time.Now().In(s.timezone)
	j.Resume(now)

	s.jobstoresMu.RLock()
	store := s.jobstores[alias]
	s.jobstoresMu.RUnlock()

	if err := store.UpdateJob(j); err != nil {
		return nil, err
	}

	s.listeners.Dispatch(event.NewJobModifiedEvent(j.ID, alias))
	s.wakeup()

	return j, nil
}

// RemoveJob unschedules a job.
func (s *BaseScheduler) RemoveJob(jobID string) error {
	_, alias, err := s.lookupJob(jobID)
	if err != nil {
		return err
	}

	s.jobstoresMu.RLock()
	store := s.jobstores[alias]
	s.jobstoresMu.RUnlock()

	if err := store.RemoveJob(jobID); err != nil {
		return err
	}

	s.listeners.Dispatch(event.NewJobRemovedEvent(jobID, alias))
	return nil
}

// RemoveAllJobs removes all jobs from all job stores.
func (s *BaseScheduler) RemoveAllJobs() error {
	s.jobstoresMu.RLock()
	defer s.jobstoresMu.RUnlock()

	for _, store := range s.jobstores {
		if err := store.RemoveAllJobs(); err != nil {
			return err
		}
	}

	return nil
}

// GetJob retrieves a job by ID.
func (s *BaseScheduler) GetJob(jobID string) (*job.Job, error) {
	j, _, err := s.lookupJob(jobID)
	return j, err
}

// GetJobs returns all scheduled jobs.
func (s *BaseScheduler) GetJobs() ([]*job.Job, error) {
	var allJobs []*job.Job

	s.jobstoresMu.RLock()
	defer s.jobstoresMu.RUnlock()

	for _, store := range s.jobstores {
		jobs, err := store.GetAllJobs()
		if err != nil {
			return nil, err
		}
		allJobs = append(allJobs, jobs...)
	}

	return allJobs, nil
}

// lookupJob finds a job across all job stores.
func (s *BaseScheduler) lookupJob(jobID string) (*job.Job, string, error) {
	s.jobstoresMu.RLock()
	defer s.jobstoresMu.RUnlock()

	for alias, store := range s.jobstores {
		j, err := store.LookupJob(jobID)
		if err != nil {
			return nil, "", err
		}
		if j != nil {
			return j, alias, nil
		}
	}

	return nil, "", nil
}

// Pause temporarily suspends job processing.
func (s *BaseScheduler) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateRunning {
		return ErrSchedulerNotRunning
	}

	s.state = StatePaused
	s.listeners.Dispatch(event.NewSchedulerPausedEvent())
	return nil
}

// Resume resumes job processing.
func (s *BaseScheduler) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StatePaused {
		return ErrSchedulerNotRunning
	}

	s.state = StateRunning
	s.listeners.Dispatch(event.NewSchedulerResumedEvent())
	s.wakeup()
	return nil
}

// ProcessJobs is the main job processing function.
// Returns the duration to wait until the next job is due, or nil for infinite wait.
func (s *BaseScheduler) ProcessJobs() *time.Duration {
	s.mu.RLock()
	if s.state == StatePaused {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	now := time.Now().In(s.timezone)
	var nextWakeup *time.Time
	var retryWakeup *time.Time
	skippedDue := false

	s.jobstoresMu.RLock()
	defer s.jobstoresMu.RUnlock()

	for alias, store := range s.jobstores {
		dueJobs, err := store.GetDueJobs(now)
		if err != nil {
			// Schedule retry
			retryTime := now.Add(s.jobStoreRetryInterval)
			if nextWakeup == nil || retryTime.Before(*nextWakeup) {
				nextWakeup = &retryTime
			}
			continue
		}

		for _, j := range dueJobs {
			if retryAt, ok := s.getSubmitRetry(j.ID); ok {
				if retryAt.After(now) {
					skippedDue = true
					if retryWakeup == nil || retryAt.Before(*retryWakeup) {
						retryWakeup = &retryAt
					}
					continue
				}
				s.clearSubmitRetry(j.ID)
			}

			s.executeJob(j, alias, now, store)
		}

		// Check for next wakeup from this store
		storeNext := store.GetNextRunTime()
		if storeNext != nil {
			if storeNext.Before(now) && skippedDue {
				continue
			}
			if nextWakeup == nil || storeNext.Before(*nextWakeup) {
				nextWakeup = storeNext
			}
		}
	}

	if retryWakeup != nil && (nextWakeup == nil || retryWakeup.Before(*nextWakeup)) {
		nextWakeup = retryWakeup
	}

	if nextWakeup == nil {
		return nil
	}

	wait := time.Until(*nextWakeup)
	if wait < 0 {
		wait = 0
	}
	return &wait
}

// executeJob executes a single job.
func (s *BaseScheduler) executeJob(j *job.Job, alias string, now time.Time, store jobstore.JobStore) {
	j.SetJobStoreAlias(alias)

	_ = j.RestoreTriggerFromRaw()
	if isNilInterface(j.Trigger) {
		s.pauseJobWithError(j, alias, store, now, fmt.Errorf("job trigger is nil or failed to decode"))
		return
	}

	if isNilInterface(j.Func) && j.FuncRef != "" {
		if fn, ok := job.LookupFunc(j.FuncRef); ok {
			j.Func = fn
		}
	}
	if isNilInterface(j.Func) {
		ref := j.FuncRef
		if ref == "" {
			ref = "<unknown>"
		}
		s.pauseJobWithError(j, alias, store, now, fmt.Errorf("job function %q is not registered", ref))
		return
	}

	s.executorsMu.RLock()
	exec, exists := s.executors[j.Executor]
	s.executorsMu.RUnlock()

	if !exists {
		s.pauseJobWithError(j, alias, store, now, fmt.Errorf("executor %q not registered", j.Executor))
		return
	}

	// Calculate run times
	runTimes := j.GetRunTimes(now)
	if j.Coalesce && len(runTimes) > 1 {
		runTimes = runTimes[len(runTimes)-1:]
	}

	if len(runTimes) == 0 {
		return
	}

	// Submit to executor
	err := exec.SubmitJob(j, runTimes)
	if err != nil {
		if _, ok := err.(*executor.MaxInstancesReachedError); ok {
			s.listeners.Dispatch(event.NewJobMaxInstancesEvent(j.ID, alias, runTimes))
		} else {
			s.listeners.Dispatch(event.NewJobErrorEvent(j.ID, alias, runTimes[0], err, ""))
			s.setSubmitRetry(j.ID, now.Add(s.submitRetryDelay()))
			return
		}
	} else {
		s.listeners.Dispatch(event.NewJobSubmittedEvent(j.ID, alias, runTimes))
	}

	// Update next run time
	lastRunTime := runTimes[len(runTimes)-1]
	nextRunTime := j.Trigger.GetNextFireTime(&lastRunTime, now)

	if nextRunTime != nil {
		j.SetNextRunTime(nextRunTime)
		store.UpdateJob(j)
	} else {
		store.RemoveJob(j.ID)
		s.listeners.Dispatch(event.NewJobRemovedEvent(j.ID, alias))
	}
}

func (s *BaseScheduler) pauseJobWithError(j *job.Job, alias string, store jobstore.JobStore, now time.Time, err error) {
	s.clearSubmitRetry(j.ID)

	updated := false
	if j.GetNextRunTime() != nil {
		j.Pause()
		if store.UpdateJob(j) == nil {
			updated = true
		}
	}

	if updated {
		s.listeners.Dispatch(event.NewJobModifiedEvent(j.ID, alias))
	}

	s.listeners.Dispatch(event.NewJobErrorEvent(j.ID, alias, now, err, ""))
}

func (s *BaseScheduler) validateJobsOnStart() {
	s.jobstoresMu.RLock()
	stores := make(map[string]jobstore.JobStore, len(s.jobstores))
	for alias, store := range s.jobstores {
		stores[alias] = store
	}
	s.jobstoresMu.RUnlock()

	now := time.Now().In(s.timezone)

	for alias, store := range stores {
		jobs, err := store.GetAllJobs()
		if err != nil {
			continue
		}
		for _, j := range jobs {
			if j == nil {
				continue
			}

			j.SetJobStoreAlias(alias)

			_ = j.RestoreTriggerFromRaw()
			if isNilInterface(j.Trigger) {
				s.pauseJobWithError(j, alias, store, now, fmt.Errorf("job trigger is nil or failed to decode"))
				continue
			}

			if isNilInterface(j.Func) && j.FuncRef != "" {
				if fn, ok := job.LookupFunc(j.FuncRef); ok {
					j.Func = fn
				}
			}
			if isNilInterface(j.Func) {
				ref := j.FuncRef
				if ref == "" {
					ref = "<unknown>"
				}
				s.pauseJobWithError(j, alias, store, now, fmt.Errorf("job function %q is not registered", ref))
				continue
			}

			if !s.hasExecutor(j.Executor) {
				s.pauseJobWithError(j, alias, store, now, fmt.Errorf("executor %q not registered", j.Executor))
			}
		}
	}
}

func (s *BaseScheduler) hasExecutor(alias string) bool {
	s.executorsMu.RLock()
	defer s.executorsMu.RUnlock()
	_, ok := s.executors[alias]
	return ok
}

func isNilInterface(value interface{}) bool {
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

func (s *BaseScheduler) submitRetryDelay() time.Duration {
	if s.jobStoreRetryInterval <= 0 {
		return time.Second
	}
	return s.jobStoreRetryInterval
}

func (s *BaseScheduler) getSubmitRetry(jobID string) (time.Time, bool) {
	s.submitRetryMu.Lock()
	defer s.submitRetryMu.Unlock()
	retryAt, ok := s.submitRetry[jobID]
	return retryAt, ok
}

func (s *BaseScheduler) setSubmitRetry(jobID string, retryAt time.Time) {
	s.submitRetryMu.Lock()
	defer s.submitRetryMu.Unlock()
	s.submitRetry[jobID] = retryAt
}

func (s *BaseScheduler) clearSubmitRetry(jobID string) {
	s.submitRetryMu.Lock()
	defer s.submitRetryMu.Unlock()
	delete(s.submitRetry, jobID)
}

// wakeup signals the main loop to wake up.
func (s *BaseScheduler) wakeup() {
	select {
	case s.wakeupCh <- struct{}{}:
	default:
	}
}

// startComponents initializes executors and job stores.
func (s *BaseScheduler) startComponents() error {
	s.executorsMu.Lock()
	// Create default executor if none exists
	if len(s.executors) == 0 {
		s.executors["default"] = executor.NewGoroutinePoolExecutor(10)
	}

	for alias, exec := range s.executors {
		if err := exec.Start(s, alias); err != nil {
			s.executorsMu.Unlock()
			return err
		}
	}
	s.executorsMu.Unlock()

	s.jobstoresMu.Lock()
	// Create default job store if none exists
	if len(s.jobstores) == 0 {
		s.jobstores["default"] = jobstore.NewMemoryJobStore()
	}

	for alias, store := range s.jobstores {
		if err := store.Start(s, alias); err != nil {
			s.jobstoresMu.Unlock()
			return err
		}
	}
	s.jobstoresMu.Unlock()

	// Add pending jobs
	s.pendingJobsMu.Lock()
	for _, pj := range s.pendingJobs {
		if err := s.addJobToStore(pj.job, pj.jobstoreAlias); err != nil {
			s.pendingJobsMu.Unlock()
			return fmt.Errorf("failed to add pending job %s: %w", pj.job.ID, err)
		}
	}
	s.pendingJobs = nil
	s.pendingJobsMu.Unlock()

	s.validateJobsOnStart()

	return nil
}

// stopComponents shuts down executors and job stores.
func (s *BaseScheduler) stopComponents(wait bool) {
	s.executorsMu.Lock()
	for _, exec := range s.executors {
		exec.Shutdown(wait)
	}
	s.executorsMu.Unlock()

	s.jobstoresMu.Lock()
	for _, store := range s.jobstores {
		store.Shutdown()
	}
	s.jobstoresMu.Unlock()
}
