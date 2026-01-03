package jobstore

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/georgepadayatti/goscheduler/job"
	jobtrigger "github.com/georgepadayatti/goscheduler/trigger"
)

// RedisJobStore stores jobs in Redis.
// Uses a hash for job data and a sorted set for run times.
type RedisJobStore struct {
	client      *redis.Client
	jobsKey     string
	runTimesKey string
	scheduler   any
	alias       string
	ctx         context.Context
}

// RedisJobStoreConfig configures the Redis job store.
type RedisJobStoreConfig struct {
	// Existing Redis client (takes precedence)
	Client *redis.Client

	// Connection options (used if Client is nil)
	Addr     string // e.g., "localhost:6379"
	Password string
	DB       int

	// Key names
	JobsKey     string // default: "goscheduler.jobs"
	RunTimesKey string // default: "goscheduler.run_times"
}

// NewRedisJobStore creates a new Redis job store.
func NewRedisJobStore(cfg RedisJobStoreConfig) (*RedisJobStore, error) {
	var client *redis.Client

	if cfg.Client != nil {
		client = cfg.Client
	} else {
		if cfg.Addr == "" {
			cfg.Addr = "localhost:6379"
		}
		client = redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
	}

	jobsKey := cfg.JobsKey
	if jobsKey == "" {
		jobsKey = "goscheduler.jobs"
	}

	runTimesKey := cfg.RunTimesKey
	if runTimesKey == "" {
		runTimesKey = "goscheduler.run_times"
	}

	return &RedisJobStore{
		client:      client,
		jobsKey:     jobsKey,
		runTimesKey: runTimesKey,
		ctx:         context.Background(),
	}, nil
}

// Start initializes the job store.
func (s *RedisJobStore) Start(scheduler any, alias string) error {
	s.scheduler = scheduler
	s.alias = alias

	// Test connection
	if err := s.client.Ping(s.ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return nil
}

// Shutdown closes the Redis connection.
func (s *RedisJobStore) Shutdown() error {
	return s.client.Close()
}

// LookupJob returns a job by ID.
func (s *RedisJobStore) LookupJob(jobID string) (*job.Job, error) {
	data, err := s.client.HGet(s.ctx, s.jobsKey, jobID).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lookup job: %w", err)
	}

	return s.deserializeJob(data)
}

// GetDueJobs returns jobs with next_run_time <= now.
func (s *RedisJobStore) GetDueJobs(now time.Time) ([]*job.Job, error) {
	timestamp := float64(now.UTC().Unix()) + float64(now.Nanosecond())/1e9

	// Get job IDs from sorted set where score <= timestamp
	jobIDs, err := s.client.ZRangeByScore(s.ctx, s.runTimesKey, &redis.ZRangeBy{
		Min: "0",
		Max: fmt.Sprintf("%f", timestamp),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get due job IDs: %w", err)
	}

	if len(jobIDs) == 0 {
		return nil, nil
	}

	// Get job data for each ID
	jobDataList, err := s.client.HMGet(s.ctx, s.jobsKey, jobIDs...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get job data: %w", err)
	}

	var jobs []*job.Job
	for i, data := range jobDataList {
		if data == nil {
			continue
		}
		dataBytes, ok := data.(string)
		if !ok {
			continue
		}
		j, err := s.deserializeJob([]byte(dataBytes))
		if err != nil {
			// Log and skip corrupted jobs
			s.removeCorruptedJob(jobIDs[i])
			continue
		}
		jobs = append(jobs, j)
	}

	return jobs, nil
}

// GetNextRunTime returns the earliest next run time.
func (s *RedisJobStore) GetNextRunTime() *time.Time {
	result, err := s.client.ZRangeWithScores(s.ctx, s.runTimesKey, 0, 0).Result()
	if err != nil || len(result) == 0 {
		return nil
	}

	timestamp := result[0].Score
	sec := int64(timestamp)
	nsec := int64((timestamp - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec).UTC()
	return &t
}

// GetAllJobs returns all jobs sorted by next run time.
func (s *RedisJobStore) GetAllJobs() ([]*job.Job, error) {
	// Get all job data from hash
	jobData, err := s.client.HGetAll(s.ctx, s.jobsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get all jobs: %w", err)
	}

	var jobs []*job.Job
	for jobID, data := range jobData {
		j, err := s.deserializeJob([]byte(data))
		if err != nil {
			s.removeCorruptedJob(jobID)
			continue
		}
		jobs = append(jobs, j)
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
func (s *RedisJobStore) AddJob(j *job.Job) error {
	// Check for function reference
	if j.FuncRef == "" {
		return &TransientJobError{JobID: j.ID}
	}

	// Check if job already exists
	exists, err := s.client.HExists(s.ctx, s.jobsKey, j.ID).Result()
	if err != nil {
		return fmt.Errorf("failed to check job existence: %w", err)
	}
	if exists {
		return &ConflictingIDError{JobID: j.ID}
	}

	// Serialize job
	data, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	// Use pipeline for atomic operation
	pipe := s.client.Pipeline()

	// Add job data to hash
	pipe.HSet(s.ctx, s.jobsKey, j.ID, data)

	// Add to sorted set if not paused
	if j.NextRunTime != nil {
		timestamp := float64(j.NextRunTime.UTC().Unix()) + float64(j.NextRunTime.Nanosecond())/1e9
		pipe.ZAdd(s.ctx, s.runTimesKey, redis.Z{
			Score:  timestamp,
			Member: j.ID,
		})
	}

	_, err = pipe.Exec(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to add job: %w", err)
	}

	return nil
}

// UpdateJob updates an existing job.
func (s *RedisJobStore) UpdateJob(j *job.Job) error {
	// Check if job exists
	exists, err := s.client.HExists(s.ctx, s.jobsKey, j.ID).Result()
	if err != nil {
		return fmt.Errorf("failed to check job existence: %w", err)
	}
	if !exists {
		return &JobLookupError{JobID: j.ID}
	}

	// Serialize job
	data, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	// Use pipeline
	pipe := s.client.Pipeline()

	// Update job data
	pipe.HSet(s.ctx, s.jobsKey, j.ID, data)

	// Update sorted set
	if j.NextRunTime != nil {
		timestamp := float64(j.NextRunTime.UTC().Unix()) + float64(j.NextRunTime.Nanosecond())/1e9
		pipe.ZAdd(s.ctx, s.runTimesKey, redis.Z{
			Score:  timestamp,
			Member: j.ID,
		})
	} else {
		pipe.ZRem(s.ctx, s.runTimesKey, j.ID)
	}

	_, err = pipe.Exec(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	return nil
}

// RemoveJob removes a job by ID.
func (s *RedisJobStore) RemoveJob(jobID string) error {
	// Check if job exists
	exists, err := s.client.HExists(s.ctx, s.jobsKey, jobID).Result()
	if err != nil {
		return fmt.Errorf("failed to check job existence: %w", err)
	}
	if !exists {
		return &JobLookupError{JobID: jobID}
	}

	// Use pipeline
	pipe := s.client.Pipeline()
	pipe.HDel(s.ctx, s.jobsKey, jobID)
	pipe.ZRem(s.ctx, s.runTimesKey, jobID)

	_, err = pipe.Exec(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to remove job: %w", err)
	}

	return nil
}

// RemoveAllJobs removes all jobs.
func (s *RedisJobStore) RemoveAllJobs() error {
	pipe := s.client.Pipeline()
	pipe.Del(s.ctx, s.jobsKey)
	pipe.Del(s.ctx, s.runTimesKey)

	_, err := pipe.Exec(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to remove all jobs: %w", err)
	}

	return nil
}

// serializeJob serializes a job to bytes.
func (s *RedisJobStore) serializeJob(j *job.Job) ([]byte, error) {
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
func (s *RedisJobStore) deserializeJob(data []byte) (*job.Job, error) {
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

// removeCorruptedJob removes a corrupted job from the store.
func (s *RedisJobStore) removeCorruptedJob(jobID string) {
	pipe := s.client.Pipeline()
	pipe.HDel(s.ctx, s.jobsKey, jobID)
	pipe.ZRem(s.ctx, s.runTimesKey, jobID)
	pipe.Exec(s.ctx)
}
