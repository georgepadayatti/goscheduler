package job

import (
	"errors"
	"time"
)

// Option is a functional option for configuring a Job.
type Option func(*Job) error

// WithID sets the job's unique identifier.
func WithID(id string) Option {
	return func(j *Job) error {
		if id == "" {
			return errors.New("job id must be a non-empty string")
		}
		j.ID = id
		return nil
	}
}

// WithName sets the job's human-readable name.
func WithName(name string) Option {
	return func(j *Job) error {
		if name == "" {
			return errors.New("job name must be a non-empty string")
		}
		j.Name = name
		return nil
	}
}

// WithArgs sets the positional arguments for the job function.
func WithArgs(args ...interface{}) Option {
	return func(j *Job) error {
		j.Args = args
		return nil
	}
}

// WithExecutor sets the executor alias for this job.
func WithExecutor(alias string) Option {
	return func(j *Job) error {
		if alias == "" {
			return errors.New("executor alias must be a non-empty string")
		}
		j.Executor = alias
		return nil
	}
}

// WithJobStore sets the job store alias for this job.
func WithJobStore(alias string) Option {
	return func(j *Job) error {
		if alias == "" {
			return errors.New("job store alias must be a non-empty string")
		}
		j.JobStore = alias
		return nil
	}
}

// WithCoalesce sets whether to combine multiple overdue runs into one.
func WithCoalesce(coalesce bool) Option {
	return func(j *Job) error {
		j.Coalesce = coalesce
		return nil
	}
}

// WithMisfireGraceTime sets the grace period for late job execution.
// A nil value means the job will run no matter how late it is.
func WithMisfireGraceTime(d time.Duration) Option {
	return func(j *Job) error {
		if d < 0 {
			return errors.New("misfire grace time must be positive")
		}
		j.MisfireGraceTime = &d
		return nil
	}
}

// WithNoMisfireGraceTime removes the misfire grace time limit.
// This means the job will run no matter how late it is.
func WithNoMisfireGraceTime() Option {
	return func(j *Job) error {
		j.MisfireGraceTime = nil
		return nil
	}
}

// WithMaxInstances sets the maximum number of concurrent instances.
func WithMaxInstances(max int) Option {
	return func(j *Job) error {
		if max <= 0 {
			return errors.New("max instances must be a positive integer")
		}
		j.MaxInstances = max
		return nil
	}
}

// WithNextRunTime sets the initial next run time.
func WithNextRunTime(t time.Time) Option {
	return func(j *Job) error {
		j.NextRunTime = &t
		return nil
	}
}

// WithFuncRef sets the function reference string for serialization.
// If not set, a reference is derived from the function name.
func WithFuncRef(ref string) Option {
	return func(j *Job) error {
		j.FuncRef = ref
		return nil
	}
}

// Modify applies the given options to an existing job.
// This is used internally by the scheduler's ModifyJob method.
func (j *Job) Modify(opts ...Option) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	for _, opt := range opts {
		// Create a temporary job to validate the option
		if err := opt(j); err != nil {
			return err
		}
	}

	return nil
}

// UpdateNextRunTime updates the job's next run time based on the trigger.
func (j *Job) UpdateNextRunTime(now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if !isNilInterface(j.Trigger) {
		j.NextRunTime = j.Trigger.GetNextFireTime(j.NextRunTime, now)
	}
}

// SetNextRunTime sets the next run time directly.
func (j *Job) SetNextRunTime(t *time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.NextRunTime = t
}

// GetNextRunTime returns the next run time.
func (j *Job) GetNextRunTime() *time.Time {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if j.NextRunTime == nil {
		return nil
	}
	t := *j.NextRunTime
	return &t
}

// Pause pauses the job by setting next_run_time to nil.
func (j *Job) Pause() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.NextRunTime = nil
}

// Resume resumes a paused job by calculating the next run time.
func (j *Job) Resume(now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if !isNilInterface(j.Trigger) {
		j.NextRunTime = j.Trigger.GetNextFireTime(nil, now)
	}
}
