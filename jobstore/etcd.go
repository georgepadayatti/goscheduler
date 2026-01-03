package jobstore

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"sort"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/georgepadayatti/goscheduler/job"
	jobtrigger "github.com/georgepadayatti/goscheduler/trigger"
)

// EtcdJobStore stores jobs in an etcd cluster.
type EtcdJobStore struct {
	client    *clientv3.Client
	path      string
	scheduler any
	alias     string
	ctx       context.Context
	ownsClient bool
}

// EtcdJobStoreConfig configures the etcd job store.
type EtcdJobStoreConfig struct {
	// Existing etcd client (takes precedence)
	Client *clientv3.Client

	// Etcd endpoints (used if Client is nil)
	// e.g., []string{"localhost:2379"}
	Endpoints []string

	// Connection timeout
	DialTimeout time.Duration

	// Username and password for authentication
	Username string
	Password string

	// Path prefix for storing jobs (default: "/goscheduler/jobs")
	Path string
}

// NewEtcdJobStore creates a new etcd job store.
func NewEtcdJobStore(cfg EtcdJobStoreConfig) (*EtcdJobStore, error) {
	var client *clientv3.Client
	var err error
	var ownsClient bool

	if cfg.Client != nil {
		client = cfg.Client
		ownsClient = false
	} else {
		if len(cfg.Endpoints) == 0 {
			cfg.Endpoints = []string{"localhost:2379"}
		}
		if cfg.DialTimeout == 0 {
			cfg.DialTimeout = 5 * time.Second
		}

		clientCfg := clientv3.Config{
			Endpoints:   cfg.Endpoints,
			DialTimeout: cfg.DialTimeout,
		}
		if cfg.Username != "" {
			clientCfg.Username = cfg.Username
			clientCfg.Password = cfg.Password
		}

		client, err = clientv3.New(clientCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to etcd: %w", err)
		}
		ownsClient = true
	}

	path := cfg.Path
	if path == "" {
		path = "/goscheduler/jobs"
	}

	return &EtcdJobStore{
		client:     client,
		path:       path,
		ctx:        context.Background(),
		ownsClient: ownsClient,
	}, nil
}

// Start initializes the job store.
func (s *EtcdJobStore) Start(scheduler any, alias string) error {
	s.scheduler = scheduler
	s.alias = alias

	// Test connection by checking cluster status
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	_, err := s.client.Status(ctx, s.client.Endpoints()[0])
	if err != nil {
		return fmt.Errorf("failed to connect to etcd: %w", err)
	}

	return nil
}

// Shutdown closes the etcd connection if we own it.
func (s *EtcdJobStore) Shutdown() error {
	if s.ownsClient && s.client != nil {
		return s.client.Close()
	}
	return nil
}

// keyForJob returns the etcd key for a job.
func (s *EtcdJobStore) keyForJob(jobID string) string {
	return fmt.Sprintf("%s/%s", s.path, jobID)
}

// LookupJob returns a job by ID.
func (s *EtcdJobStore) LookupJob(jobID string) (*job.Job, error) {
	key := s.keyForJob(jobID)

	resp, err := s.client.Get(s.ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup job: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return nil, nil
	}

	return s.deserializeJob(resp.Kvs[0].Value)
}

// GetDueJobs returns jobs with next_run_time <= now.
func (s *EtcdJobStore) GetDueJobs(now time.Time) ([]*job.Job, error) {
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
func (s *EtcdJobStore) GetNextRunTime() *time.Time {
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
func (s *EtcdJobStore) GetAllJobs() ([]*job.Job, error) {
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
func (s *EtcdJobStore) AddJob(j *job.Job) error {
	if j.FuncRef == "" {
		return &TransientJobError{JobID: j.ID}
	}

	key := s.keyForJob(j.ID)

	data, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	// Use transaction to ensure job doesn't already exist
	txnResp, err := s.client.Txn(s.ctx).
		If(clientv3.Compare(clientv3.Version(key), "=", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()

	if err != nil {
		return fmt.Errorf("failed to add job: %w", err)
	}

	if !txnResp.Succeeded {
		return &ConflictingIDError{JobID: j.ID}
	}

	return nil
}

// UpdateJob updates an existing job.
func (s *EtcdJobStore) UpdateJob(j *job.Job) error {
	key := s.keyForJob(j.ID)

	data, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	// Use transaction to ensure job exists
	txnResp, err := s.client.Txn(s.ctx).
		If(clientv3.Compare(clientv3.Version(key), ">", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()

	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	if !txnResp.Succeeded {
		return &JobLookupError{JobID: j.ID}
	}

	return nil
}

// RemoveJob removes a job by ID.
func (s *EtcdJobStore) RemoveJob(jobID string) error {
	key := s.keyForJob(jobID)

	// Use transaction to ensure job exists before deleting
	txnResp, err := s.client.Txn(s.ctx).
		If(clientv3.Compare(clientv3.Version(key), ">", 0)).
		Then(clientv3.OpDelete(key)).
		Commit()

	if err != nil {
		return fmt.Errorf("failed to remove job: %w", err)
	}

	if !txnResp.Succeeded {
		return &JobLookupError{JobID: jobID}
	}

	return nil
}

// RemoveAllJobs removes all jobs.
func (s *EtcdJobStore) RemoveAllJobs() error {
	_, err := s.client.Delete(s.ctx, s.path, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to remove all jobs: %w", err)
	}
	return nil
}

// jobRecord holds job data with next run time for sorting.
type jobRecord struct {
	Job         *job.Job
	NextRunTime *float64
}

// getAllJobRecords returns all job records from etcd.
func (s *EtcdJobStore) getAllJobRecords() ([]jobRecord, error) {
	resp, err := s.client.Get(s.ctx, s.path, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs: %w", err)
	}

	var records []jobRecord
	var failedKeys []string

	for _, kv := range resp.Kvs {
		j, err := s.deserializeJob(kv.Value)
		if err != nil {
			failedKeys = append(failedKeys, string(kv.Key))
			continue
		}

		var nextRunTime *float64
		if j.NextRunTime != nil {
			ts := float64(j.NextRunTime.UTC().Unix()) + float64(j.NextRunTime.Nanosecond())/1e9
			nextRunTime = &ts
		}

		records = append(records, jobRecord{
			Job:         j,
			NextRunTime: nextRunTime,
		})
	}

	// Remove corrupted jobs
	for _, key := range failedKeys {
		s.client.Delete(s.ctx, key)
	}

	return records, nil
}

// serializeJob serializes a job to bytes.
func (s *EtcdJobStore) serializeJob(j *job.Job) ([]byte, error) {
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
func (s *EtcdJobStore) deserializeJob(data []byte) (*job.Job, error) {
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
