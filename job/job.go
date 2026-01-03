// Package job provides the Job type that represents a scheduled job.
package job

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Trigger is the interface that triggers must implement.
// This is defined here to avoid circular imports.
type Trigger interface {
	GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time
	String() string
}

// Job represents a scheduled job with all its configuration and state.
type Job struct {
	mu sync.RWMutex

	// Unique identifier for this job
	ID string

	// Human-readable name for this job
	Name string

	// The function to execute (stored as interface{} for flexibility)
	Func interface{}

	// String reference to the function (for serialization).
	// Derived from the function name if not provided.
	FuncRef string

	// Positional arguments to pass to the function
	Args []interface{}

	// The trigger that determines when this job runs
	Trigger Trigger

	// Internal: raw trigger data retained when decoding fails
	rawTriggerData []byte
	rawTriggerType string

	// Name of the executor to use (default: "default")
	Executor string

	// Name of the job store (default: "default")
	JobStore string

	// Next scheduled run time (nil if paused)
	NextRunTime *time.Time

	// If true, combine multiple overdue runs into one
	Coalesce bool

	// Grace period for late execution (nil means run no matter how late)
	MisfireGraceTime *time.Duration

	// Maximum concurrent instances of this job
	MaxInstances int

	// Internal: reference to the scheduler (not serialized)
	scheduler interface{}

	// Internal: the job store alias this job is stored in
	jobStoreAlias string
}

// JobState represents the serializable state of a job.
type JobState struct {
	Version          int
	ID               string
	Name             string
	FuncRef          string
	Args             []interface{}
	TriggerData      []byte
	TriggerType      string
	Executor         string
	JobStore         string
	Coalesce         bool
	MisfireGraceTime *time.Duration
	MaxInstances     int
	NextRunTime      *time.Time
}

// New creates a new Job with the given function and trigger.
func New(fn interface{}, trigger Trigger, opts ...Option) (*Job, error) {
	j := &Job{
		ID:           uuid.New().String(),
		Func:         fn,
		Trigger:      trigger,
		Executor:     "default",
		JobStore:     "default",
		Coalesce:     true,
		MaxInstances: 1,
	}

	// Derive name from function if not provided
	j.Name = getFuncName(fn)

	// Apply options
	for _, opt := range opts {
		if err := opt(j); err != nil {
			return nil, err
		}
	}

	if j.FuncRef == "" {
		if ref, err := FuncRefForFunc(fn); err == nil {
			j.FuncRef = ref
		}
	}

	return j, nil
}

// getFuncName attempts to get a readable name for the function.
func getFuncName(fn interface{}) string {
	if fn == nil {
		return "<nil>"
	}
	if ref, err := FuncRefForFunc(fn); err == nil {
		return shortFuncName(ref)
	}
	return fmt.Sprintf("%T", fn)
}

func shortFuncName(ref string) string {
	if ref == "" {
		return "<unknown>"
	}
	if idx := strings.LastIndex(ref, "/"); idx >= 0 {
		return ref[idx+1:]
	}
	return ref
}

// GetRunTimes computes all scheduled run times between next_run_time and now (inclusive).
func (j *Job) GetRunTimes(now time.Time) []time.Time {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if isNilInterface(j.Trigger) || j.NextRunTime == nil {
		return nil
	}

	var runTimes []time.Time
	nextRunTime := j.NextRunTime

	for nextRunTime != nil && !nextRunTime.After(now) {
		runTimes = append(runTimes, *nextRunTime)
		nextRunTime = j.Trigger.GetNextFireTime(nextRunTime, now)
	}

	return runTimes
}

// IsPaused returns true if the job is currently paused.
func (j *Job) IsPaused() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.NextRunTime == nil
}

// IsPending returns true if the job hasn't been added to a job store yet.
func (j *Job) IsPending() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.jobStoreAlias == ""
}

// SetScheduler sets the scheduler reference (internal use only).
func (j *Job) SetScheduler(scheduler interface{}) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.scheduler = scheduler
}

// SetJobStoreAlias sets the job store alias (internal use only).
func (j *Job) SetJobStoreAlias(alias string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jobStoreAlias = alias
}

// GetJobStoreAlias returns the job store alias.
func (j *Job) GetJobStoreAlias() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.jobStoreAlias
}

// Clone creates a deep copy of the job.
func (j *Job) Clone() *Job {
	j.mu.RLock()
	defer j.mu.RUnlock()

	clone := &Job{
		ID:               j.ID,
		Name:             j.Name,
		Func:             j.Func,
		FuncRef:          j.FuncRef,
		Trigger:          j.Trigger,
		Executor:         j.Executor,
		JobStore:         j.JobStore,
		Coalesce:         j.Coalesce,
		MaxInstances:     j.MaxInstances,
		scheduler:        j.scheduler,
		jobStoreAlias:    j.jobStoreAlias,
		rawTriggerData:   append([]byte(nil), j.rawTriggerData...),
		rawTriggerType:   j.rawTriggerType,
	}

	// Deep copy Args
	if j.Args != nil {
		clone.Args = make([]interface{}, len(j.Args))
		copy(clone.Args, j.Args)
	}

	// Copy NextRunTime
	if j.NextRunTime != nil {
		t := *j.NextRunTime
		clone.NextRunTime = &t
	}

	// Copy MisfireGraceTime
	if j.MisfireGraceTime != nil {
		d := *j.MisfireGraceTime
		clone.MisfireGraceTime = &d
	}

	return clone
}

// GetState returns the serializable state of the job.
func (j *Job) GetState() (*JobState, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	state := &JobState{
		Version:          1,
		ID:               j.ID,
		Name:             j.Name,
		FuncRef:          j.FuncRef,
		Args:             j.Args,
		Executor:         j.Executor,
		JobStore:         j.JobStore,
		Coalesce:         j.Coalesce,
		MisfireGraceTime: j.MisfireGraceTime,
		MaxInstances:     j.MaxInstances,
		NextRunTime:      j.NextRunTime,
	}

	// Serialize trigger if it implements gob encoding
	if !isNilInterface(j.Trigger) {
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		if err := enc.Encode(&j.Trigger); err != nil {
			return nil, fmt.Errorf("failed to encode trigger: %w", err)
		}
		state.TriggerData = buf.Bytes()
		state.TriggerType = fmt.Sprintf("%T", j.Trigger)
	} else if len(j.rawTriggerData) > 0 {
		state.TriggerData = append([]byte(nil), j.rawTriggerData...)
		state.TriggerType = j.rawTriggerType
	}

	return state, nil
}

// SetRawTriggerData stores serialized trigger data when decoding fails.
// Intended for internal use by job stores.
func (j *Job) SetRawTriggerData(data []byte, triggerType string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if len(data) == 0 {
		j.rawTriggerData = nil
		j.rawTriggerType = ""
		return
	}

	j.rawTriggerData = append([]byte(nil), data...)
	j.rawTriggerType = triggerType
}

// RestoreTriggerFromRaw attempts to decode a trigger from stored raw data.
// Returns nil if no raw data is available or the trigger is already set.
func (j *Job) RestoreTriggerFromRaw() error {
	j.mu.RLock()
	if !isNilInterface(j.Trigger) || len(j.rawTriggerData) == 0 {
		j.mu.RUnlock()
		return nil
	}
	data := append([]byte(nil), j.rawTriggerData...)
	j.mu.RUnlock()

	var trig Trigger
	dec := gob.NewDecoder(bytes.NewBuffer(data))
	if err := dec.Decode(&trig); err != nil || isNilInterface(trig) {
		if err == nil {
			err = fmt.Errorf("decoded trigger is nil")
		}
		return err
	}

	j.mu.Lock()
	j.Trigger = trig
	j.mu.Unlock()
	return nil
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

// String returns a string representation of the job.
func (j *Job) String() string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	var status string
	if j.NextRunTime != nil {
		status = fmt.Sprintf("next run at: %s", j.NextRunTime.Format(time.RFC3339))
	} else if j.jobStoreAlias == "" {
		status = "pending"
	} else {
		status = "paused"
	}

	triggerStr := "<nil>"
	if !isNilInterface(j.Trigger) {
		triggerStr = j.Trigger.String()
	}

	return fmt.Sprintf("%s (trigger: %s, %s)", j.Name, triggerStr, status)
}

// Repr returns a detailed representation of the job.
func (j *Job) Repr() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return fmt.Sprintf("<Job (id=%s name=%s)>", j.ID, j.Name)
}
