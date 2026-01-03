package jobstore

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"sort"
	"time"

	"github.com/go-zookeeper/zk"

	"github.com/georgepadayatti/goscheduler/job"
	jobtrigger "github.com/georgepadayatti/goscheduler/trigger"
)

// ZookeeperJobStore stores jobs in a Zookeeper cluster.
type ZookeeperJobStore struct {
	conn       *zk.Conn
	path       string
	scheduler  any
	alias      string
	ownsConn   bool
	ensuredPath bool
}

// ZookeeperJobStoreConfig configures the Zookeeper job store.
type ZookeeperJobStoreConfig struct {
	// Existing Zookeeper connection (takes precedence)
	Conn *zk.Conn

	// Zookeeper servers (used if Conn is nil)
	// e.g., []string{"localhost:2181"}
	Servers []string

	// Session timeout
	SessionTimeout time.Duration

	// Path for storing jobs (default: "/goscheduler/jobs")
	Path string
}

// NewZookeeperJobStore creates a new Zookeeper job store.
func NewZookeeperJobStore(cfg ZookeeperJobStoreConfig) (*ZookeeperJobStore, error) {
	var conn *zk.Conn
	var err error
	var ownsConn bool

	if cfg.Conn != nil {
		conn = cfg.Conn
		ownsConn = false
	} else {
		if len(cfg.Servers) == 0 {
			cfg.Servers = []string{"localhost:2181"}
		}
		if cfg.SessionTimeout == 0 {
			cfg.SessionTimeout = 5 * time.Second
		}

		conn, _, err = zk.Connect(cfg.Servers, cfg.SessionTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Zookeeper: %w", err)
		}
		ownsConn = true
	}

	path := cfg.Path
	if path == "" {
		path = "/goscheduler/jobs"
	}

	return &ZookeeperJobStore{
		conn:     conn,
		path:     path,
		ownsConn: ownsConn,
	}, nil
}

// Start initializes the job store.
func (s *ZookeeperJobStore) Start(scheduler any, alias string) error {
	s.scheduler = scheduler
	s.alias = alias

	// Ensure the path exists
	return s.ensurePath()
}

// ensurePath creates the path if it doesn't exist.
func (s *ZookeeperJobStore) ensurePath() error {
	if s.ensuredPath {
		return nil
	}

	// Create path components one by one
	components := splitPath(s.path)
	currentPath := ""

	for _, component := range components {
		currentPath = currentPath + "/" + component
		exists, _, err := s.conn.Exists(currentPath)
		if err != nil {
			return fmt.Errorf("failed to check path: %w", err)
		}
		if !exists {
			_, err = s.conn.Create(currentPath, []byte{}, 0, zk.WorldACL(zk.PermAll))
			if err != nil && err != zk.ErrNodeExists {
				return fmt.Errorf("failed to create path %s: %w", currentPath, err)
			}
		}
	}

	s.ensuredPath = true
	return nil
}

// splitPath splits a path into components.
func splitPath(path string) []string {
	var components []string
	current := ""
	for _, c := range path {
		if c == '/' {
			if current != "" {
				components = append(components, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		components = append(components, current)
	}
	return components
}

// Shutdown closes the Zookeeper connection if we own it.
func (s *ZookeeperJobStore) Shutdown() error {
	if s.ownsConn && s.conn != nil {
		s.conn.Close()
	}
	return nil
}

// nodePath returns the full path for a job node.
func (s *ZookeeperJobStore) nodePath(jobID string) string {
	return fmt.Sprintf("%s/%s", s.path, jobID)
}

// LookupJob returns a job by ID.
func (s *ZookeeperJobStore) LookupJob(jobID string) (*job.Job, error) {
	if err := s.ensurePath(); err != nil {
		return nil, err
	}

	nodePath := s.nodePath(jobID)

	data, _, err := s.conn.Get(nodePath)
	if err == zk.ErrNoNode {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lookup job: %w", err)
	}

	return s.deserializeJob(data)
}

// GetDueJobs returns jobs with next_run_time <= now.
func (s *ZookeeperJobStore) GetDueJobs(now time.Time) ([]*job.Job, error) {
	timestamp := float64(now.UTC().Unix()) + float64(now.Nanosecond())/1e9

	allJobs, err := s.getAllJobRecords()
	if err != nil {
		return nil, err
	}

	var dueJobs []*job.Job
	for _, record := range allJobs {
		if record.NextRunTime != nil && *record.NextRunTime <= timestamp {
			dueJobs = append(dueJobs, record.Job)
		}
	}

	// Sort by next run time
	sort.Slice(dueJobs, func(i, k int) bool {
		if dueJobs[i].NextRunTime == nil {
			return false
		}
		if dueJobs[k].NextRunTime == nil {
			return true
		}
		return dueJobs[i].NextRunTime.Before(*dueJobs[k].NextRunTime)
	})

	return dueJobs, nil
}

// GetNextRunTime returns the earliest next run time.
func (s *ZookeeperJobStore) GetNextRunTime() *time.Time {
	allJobs, err := s.getAllJobRecords()
	if err != nil {
		return nil
	}

	var earliest *float64
	for _, record := range allJobs {
		if record.NextRunTime != nil {
			if earliest == nil || *record.NextRunTime < *earliest {
				earliest = record.NextRunTime
			}
		}
	}

	if earliest == nil {
		return nil
	}

	sec := int64(*earliest)
	nsec := int64((*earliest - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec).UTC()
	return &t
}

// GetAllJobs returns all jobs sorted by next run time.
func (s *ZookeeperJobStore) GetAllJobs() ([]*job.Job, error) {
	allJobs, err := s.getAllJobRecords()
	if err != nil {
		return nil, err
	}

	var jobs []*job.Job
	for _, record := range allJobs {
		jobs = append(jobs, record.Job)
	}

	// Sort by next run time (paused jobs last)
	sort.Slice(jobs, func(i, k int) bool {
		if jobs[i].NextRunTime == nil && jobs[k].NextRunTime == nil {
			return jobs[i].ID < jobs[k].ID
		}
		if jobs[i].NextRunTime == nil {
			return false
		}
		if jobs[k].NextRunTime == nil {
			return true
		}
		return jobs[i].NextRunTime.Before(*jobs[k].NextRunTime)
	})

	return jobs, nil
}

// AddJob adds a job to the store.
func (s *ZookeeperJobStore) AddJob(j *job.Job) error {
	if j.FuncRef == "" {
		return &TransientJobError{JobID: j.ID}
	}

	if err := s.ensurePath(); err != nil {
		return err
	}

	nodePath := s.nodePath(j.ID)

	data, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	_, err = s.conn.Create(nodePath, data, 0, zk.WorldACL(zk.PermAll))
	if err == zk.ErrNodeExists {
		return &ConflictingIDError{JobID: j.ID}
	}
	if err != nil {
		return fmt.Errorf("failed to add job: %w", err)
	}

	return nil
}

// UpdateJob updates an existing job.
func (s *ZookeeperJobStore) UpdateJob(j *job.Job) error {
	if err := s.ensurePath(); err != nil {
		return err
	}

	nodePath := s.nodePath(j.ID)

	data, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	_, err = s.conn.Set(nodePath, data, -1)
	if err == zk.ErrNoNode {
		return &JobLookupError{JobID: j.ID}
	}
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	return nil
}

// RemoveJob removes a job by ID.
func (s *ZookeeperJobStore) RemoveJob(jobID string) error {
	if err := s.ensurePath(); err != nil {
		return err
	}

	nodePath := s.nodePath(jobID)

	err := s.conn.Delete(nodePath, -1)
	if err == zk.ErrNoNode {
		return &JobLookupError{JobID: jobID}
	}
	if err != nil {
		return fmt.Errorf("failed to remove job: %w", err)
	}

	return nil
}

// RemoveAllJobs removes all jobs.
func (s *ZookeeperJobStore) RemoveAllJobs() error {
	if err := s.ensurePath(); err != nil {
		return err
	}

	children, _, err := s.conn.Children(s.path)
	if err != nil && err != zk.ErrNoNode {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	for _, child := range children {
		nodePath := s.nodePath(child)
		if err := s.conn.Delete(nodePath, -1); err != nil && err != zk.ErrNoNode {
			return fmt.Errorf("failed to delete job %s: %w", child, err)
		}
	}

	return nil
}

// zkJobRecord holds job data with next run time for sorting.
type zkJobRecord struct {
	Job         *job.Job
	NextRunTime *float64
}

// getAllJobRecords returns all job records from Zookeeper.
func (s *ZookeeperJobStore) getAllJobRecords() ([]zkJobRecord, error) {
	if err := s.ensurePath(); err != nil {
		return nil, err
	}

	children, _, err := s.conn.Children(s.path)
	if err == zk.ErrNoNode {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	var records []zkJobRecord
	var failedIDs []string

	for _, child := range children {
		nodePath := s.nodePath(child)

		data, _, err := s.conn.Get(nodePath)
		if err != nil {
			continue
		}

		j, err := s.deserializeJob(data)
		if err != nil {
			failedIDs = append(failedIDs, child)
			continue
		}

		var nextRunTime *float64
		if j.NextRunTime != nil {
			ts := float64(j.NextRunTime.UTC().Unix()) + float64(j.NextRunTime.Nanosecond())/1e9
			nextRunTime = &ts
		}

		records = append(records, zkJobRecord{
			Job:         j,
			NextRunTime: nextRunTime,
		})
	}

	// Remove corrupted jobs
	for _, id := range failedIDs {
		s.conn.Delete(s.nodePath(id), -1)
	}

	return records, nil
}

// serializeJob serializes a job to bytes.
func (s *ZookeeperJobStore) serializeJob(j *job.Job) ([]byte, error) {
	state, err := j.GetState()
	if err != nil {
		return nil, fmt.Errorf("failed to get job state: %w", err)
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(state); err != nil {
		return nil, fmt.Errorf("failed to encode job: %w", err)
	}

	return buf.Bytes(), nil
}

// deserializeJob deserializes a job from bytes.
func (s *ZookeeperJobStore) deserializeJob(data []byte) (*job.Job, error) {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var state job.JobState
	if err := dec.Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode job: %w", err)
	}

	j := &job.Job{
		ID:               state.ID,
		Name:             state.Name,
		FuncRef:          state.FuncRef,
		Args:             state.Args,
		Executor:         state.Executor,
		JobStore:         state.JobStore,
		Coalesce:         state.Coalesce,
		MisfireGraceTime: state.MisfireGraceTime,
		MaxInstances:     state.MaxInstances,
		NextRunTime:      state.NextRunTime,
	}

	if j.JobStore == "" && s.alias != "" {
		j.JobStore = s.alias
	}

	// Restore the function from the registry using FuncRef
	if state.FuncRef != "" {
		if fn, ok := job.LookupFunc(state.FuncRef); ok {
			j.Func = fn
		}
	}

	// Restore the trigger from serialized data
	if len(state.TriggerData) > 0 {
		trigBuf := bytes.NewBuffer(state.TriggerData)
		trigDec := gob.NewDecoder(trigBuf)
		var trig jobtrigger.Trigger
		if err := trigDec.Decode(&trig); err == nil && !isNilValue(trig) {
			j.Trigger = trig
		} else {
			j.SetRawTriggerData(state.TriggerData, state.TriggerType)
		}
	}

	return j, nil
}
