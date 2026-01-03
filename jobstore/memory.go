package jobstore

import (
	"math"
	"sync"
	"time"

	"github.com/georgepadayatti/goscheduler/job"
)

// jobEntry stores a job with its timestamp for sorting.
type jobEntry struct {
	job       *job.Job
	timestamp *float64 // UTC timestamp, nil for paused jobs
}

// MemoryJobStore stores jobs in memory with no persistence.
type MemoryJobStore struct {
	mu        sync.RWMutex
	jobs      []*jobEntry          // Sorted by (timestamp, job_id)
	jobsIndex map[string]*jobEntry // O(1) lookup by job ID
	scheduler interface{}
	alias     string
}

// NewMemoryJobStore creates a new in-memory job store.
func NewMemoryJobStore() *MemoryJobStore {
	return &MemoryJobStore{
		jobs:      make([]*jobEntry, 0),
		jobsIndex: make(map[string]*jobEntry),
	}
}

// Start initializes the job store.
func (s *MemoryJobStore) Start(scheduler interface{}, alias string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scheduler = scheduler
	s.alias = alias
	return nil
}

// Shutdown clears all jobs.
func (s *MemoryJobStore) Shutdown() error {
	return s.RemoveAllJobs()
}

// LookupJob returns a job by ID.
func (s *MemoryJobStore) LookupJob(jobID string) (*job.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if entry, ok := s.jobsIndex[jobID]; ok {
		return entry.job, nil
	}
	return nil, nil
}

// GetDueJobs returns jobs with next_run_time <= now.
func (s *MemoryJobStore) GetDueJobs(now time.Time) ([]*job.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nowTimestamp := toTimestamp(&now)
	var pending []*job.Job

	for _, entry := range s.jobs {
		if entry.timestamp == nil || *entry.timestamp > *nowTimestamp {
			break
		}
		pending = append(pending, entry.job)
	}

	return pending, nil
}

// GetNextRunTime returns the earliest next run time.
func (s *MemoryJobStore) GetNextRunTime() *time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.jobs) == 0 {
		return nil
	}

	// First entry with non-nil timestamp
	for _, entry := range s.jobs {
		if entry.job.NextRunTime != nil {
			t := *entry.job.NextRunTime
			return &t
		}
	}
	return nil
}

// GetAllJobs returns all jobs.
func (s *MemoryJobStore) GetAllJobs() ([]*job.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*job.Job, len(s.jobs))
	for i, entry := range s.jobs {
		jobs[i] = entry.job
	}
	return jobs, nil
}

// AddJob adds a job to the store.
func (s *MemoryJobStore) AddJob(j *job.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobsIndex[j.ID]; exists {
		return &ConflictingIDError{JobID: j.ID}
	}

	timestamp := toTimestamp(j.NextRunTime)
	entry := &jobEntry{
		job:       j,
		timestamp: timestamp,
	}

	// Find insertion index using binary search
	index := s.getJobIndex(timestamp, j.ID)

	// Insert at position
	s.jobs = append(s.jobs, nil)
	copy(s.jobs[index+1:], s.jobs[index:])
	s.jobs[index] = entry

	s.jobsIndex[j.ID] = entry
	return nil
}

// UpdateJob updates an existing job.
func (s *MemoryJobStore) UpdateJob(j *job.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldEntry, exists := s.jobsIndex[j.ID]
	if !exists {
		return &JobLookupError{JobID: j.ID}
	}

	newTimestamp := toTimestamp(j.NextRunTime)
	oldIndex := s.getJobIndex(oldEntry.timestamp, j.ID)

	// If timestamp hasn't changed, just update in place
	if timestampEqual(oldEntry.timestamp, newTimestamp) {
		s.jobs[oldIndex].job = j
		s.jobs[oldIndex].timestamp = newTimestamp
	} else {
		// Remove from old position
		s.jobs = append(s.jobs[:oldIndex], s.jobs[oldIndex+1:]...)

		// Insert at new position
		newIndex := s.getJobIndex(newTimestamp, j.ID)
		s.jobs = append(s.jobs, nil)
		copy(s.jobs[newIndex+1:], s.jobs[newIndex:])
		s.jobs[newIndex] = &jobEntry{job: j, timestamp: newTimestamp}
	}

	s.jobsIndex[j.ID] = &jobEntry{job: j, timestamp: newTimestamp}
	return nil
}

// RemoveJob removes a job by ID.
func (s *MemoryJobStore) RemoveJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.jobsIndex[jobID]
	if !exists {
		return &JobLookupError{JobID: jobID}
	}

	index := s.getJobIndex(entry.timestamp, jobID)
	s.jobs = append(s.jobs[:index], s.jobs[index+1:]...)
	delete(s.jobsIndex, jobID)

	return nil
}

// RemoveAllJobs clears all jobs.
func (s *MemoryJobStore) RemoveAllJobs() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs = s.jobs[:0]
	s.jobsIndex = make(map[string]*jobEntry)
	return nil
}

// getJobIndex returns the index of the job, or the insertion point.
func (s *MemoryJobStore) getJobIndex(timestamp *float64, jobID string) int {
	ts := math.Inf(1)
	if timestamp != nil {
		ts = *timestamp
	}

	lo, hi := 0, len(s.jobs)
	for lo < hi {
		mid := (lo + hi) / 2
		entry := s.jobs[mid]

		midTs := math.Inf(1)
		if entry.timestamp != nil {
			midTs = *entry.timestamp
		}

		switch {
		case midTs > ts:
			hi = mid
		case midTs < ts:
			lo = mid + 1
		case entry.job.ID > jobID:
			hi = mid
		case entry.job.ID < jobID:
			lo = mid + 1
		default:
			return mid // Exact match
		}
	}

	return lo
}

// toTimestamp converts a time to a float64 UTC timestamp.
func toTimestamp(t *time.Time) *float64 {
	if t == nil {
		return nil
	}
	ts := float64(t.UTC().Unix()) + float64(t.Nanosecond())/1e9
	return &ts
}

// timestampEqual compares two timestamps for equality.
func timestampEqual(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
