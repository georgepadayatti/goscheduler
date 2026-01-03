package jobstore

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/georgepadayatti/goscheduler/job"
	jobtrigger "github.com/georgepadayatti/goscheduler/trigger"
)

// SQLJobStore stores jobs in a SQL database.
type SQLJobStore struct {
	db        *sql.DB
	tableName string
	scheduler interface{}
	alias     string
}

// SQLJobStoreConfig configures the SQL job store.
type SQLJobStoreConfig struct {
	DB        *sql.DB // Existing database connection
	DSN       string  // Data source name (alternative to DB)
	Driver    string  // Driver name (required with DSN)
	TableName string  // Table name (default: "goscheduler_jobs")
}

// NewSQLJobStore creates a new SQL job store.
func NewSQLJobStore(cfg SQLJobStoreConfig) (*SQLJobStore, error) {
	var db *sql.DB
	var err error

	if cfg.DB != nil {
		db = cfg.DB
	} else if cfg.DSN != "" {
		if cfg.Driver == "" {
			return nil, fmt.Errorf("driver is required when using DSN")
		}
		db, err = sql.Open(cfg.Driver, cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
	} else {
		return nil, fmt.Errorf("either DB or DSN must be provided")
	}

	tableName := cfg.TableName
	if tableName == "" {
		tableName = "goscheduler_jobs"
	}

	return &SQLJobStore{
		db:        db,
		tableName: tableName,
	}, nil
}

// Start initializes the job store and creates the table if needed.
func (s *SQLJobStore) Start(scheduler interface{}, alias string) error {
	s.scheduler = scheduler
	s.alias = alias

	// Create table if not exists
	// Using a generic SQL syntax that works with most databases
	_, err := s.db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(191) PRIMARY KEY,
			next_run_time DOUBLE PRECISION,
			job_state BLOB NOT NULL
		)
	`, s.tableName))
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Create index on next_run_time
	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_next_run_time
		ON %s (next_run_time)
	`, s.tableName, s.tableName))
	// Ignore index creation errors as some databases might not support this syntax
	_ = err

	return nil
}

// Shutdown releases database resources.
func (s *SQLJobStore) Shutdown() error {
	// Don't close the connection if it was provided externally
	return nil
}

// LookupJob returns a job by ID.
func (s *SQLJobStore) LookupJob(jobID string) (*job.Job, error) {
	row := s.db.QueryRow(
		fmt.Sprintf("SELECT job_state FROM %s WHERE id = ?", s.tableName),
		jobID,
	)

	var stateData []byte
	err := row.Scan(&stateData)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lookup job: %w", err)
	}

	return s.deserializeJob(stateData)
}

// GetDueJobs returns jobs with next_run_time <= now.
func (s *SQLJobStore) GetDueJobs(now time.Time) ([]*job.Job, error) {
	nowTimestamp := float64(now.UTC().Unix()) + float64(now.Nanosecond())/1e9

	rows, err := s.db.Query(
		fmt.Sprintf(
			"SELECT job_state FROM %s WHERE next_run_time IS NOT NULL AND next_run_time <= ? ORDER BY next_run_time ASC",
			s.tableName,
		),
		nowTimestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get due jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*job.Job
	for rows.Next() {
		var stateData []byte
		if err := rows.Scan(&stateData); err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}

		j, err := s.deserializeJob(stateData)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}

	return jobs, rows.Err()
}

// GetNextRunTime returns the earliest next run time.
func (s *SQLJobStore) GetNextRunTime() *time.Time {
	row := s.db.QueryRow(
		fmt.Sprintf(
			"SELECT next_run_time FROM %s WHERE next_run_time IS NOT NULL ORDER BY next_run_time ASC LIMIT 1",
			s.tableName,
		),
	)

	var timestamp float64
	err := row.Scan(&timestamp)
	if err != nil {
		return nil
	}

	sec := int64(timestamp)
	nsec := int64((timestamp - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec).UTC()
	return &t
}

// GetAllJobs returns all jobs.
func (s *SQLJobStore) GetAllJobs() ([]*job.Job, error) {
	rows, err := s.db.Query(
		fmt.Sprintf(
			"SELECT job_state FROM %s ORDER BY next_run_time ASC NULLS LAST, id ASC",
			s.tableName,
		),
	)
	if err != nil {
		// Try alternative syntax for databases that don't support NULLS LAST
		rows, err = s.db.Query(
			fmt.Sprintf(
				"SELECT job_state FROM %s ORDER BY CASE WHEN next_run_time IS NULL THEN 1 ELSE 0 END, next_run_time ASC, id ASC",
				s.tableName,
			),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get all jobs: %w", err)
		}
	}
	defer rows.Close()

	var jobs []*job.Job
	for rows.Next() {
		var stateData []byte
		if err := rows.Scan(&stateData); err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}

		j, err := s.deserializeJob(stateData)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}

	return jobs, rows.Err()
}

// AddJob adds a job to the store.
func (s *SQLJobStore) AddJob(j *job.Job) error {
	// Check for function reference (required for serialization)
	if j.FuncRef == "" {
		return &TransientJobError{JobID: j.ID}
	}

	stateData, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	var timestamp *float64
	if j.NextRunTime != nil {
		ts := float64(j.NextRunTime.UTC().Unix()) + float64(j.NextRunTime.Nanosecond())/1e9
		timestamp = &ts
	}

	_, err = s.db.Exec(
		fmt.Sprintf("INSERT INTO %s (id, next_run_time, job_state) VALUES (?, ?, ?)", s.tableName),
		j.ID,
		timestamp,
		stateData,
	)

	if err != nil {
		// Check for unique constraint violation
		if isUniqueViolation(err) {
			return &ConflictingIDError{JobID: j.ID}
		}
		return fmt.Errorf("failed to add job: %w", err)
	}

	return nil
}

// UpdateJob updates an existing job.
func (s *SQLJobStore) UpdateJob(j *job.Job) error {
	stateData, err := s.serializeJob(j)
	if err != nil {
		return err
	}

	var timestamp *float64
	if j.NextRunTime != nil {
		ts := float64(j.NextRunTime.UTC().Unix()) + float64(j.NextRunTime.Nanosecond())/1e9
		timestamp = &ts
	}

	result, err := s.db.Exec(
		fmt.Sprintf("UPDATE %s SET next_run_time = ?, job_state = ? WHERE id = ?", s.tableName),
		timestamp,
		stateData,
		j.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return &JobLookupError{JobID: j.ID}
	}

	return nil
}

// RemoveJob removes a job by ID.
func (s *SQLJobStore) RemoveJob(jobID string) error {
	result, err := s.db.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.tableName),
		jobID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove job: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return &JobLookupError{JobID: jobID}
	}

	return nil
}

// RemoveAllJobs removes all jobs.
func (s *SQLJobStore) RemoveAllJobs() error {
	_, err := s.db.Exec(fmt.Sprintf("DELETE FROM %s", s.tableName))
	if err != nil {
		return fmt.Errorf("failed to remove all jobs: %w", err)
	}
	return nil
}

// serializeJob serializes a job to bytes using gob.
func (s *SQLJobStore) serializeJob(j *job.Job) ([]byte, error) {
	state, err := j.GetState()
	if err != nil {
		return nil, fmt.Errorf("failed to get job state: %w", err)
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(state); err != nil {
		return nil, fmt.Errorf("failed to encode job state: %w", err)
	}

	return buf.Bytes(), nil
}

// deserializeJob deserializes a job from bytes.
func (s *SQLJobStore) deserializeJob(data []byte) (*job.Job, error) {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var state job.JobState
	if err := dec.Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode job state: %w", err)
	}

	// Reconstruct job from state
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

// isUniqueViolation checks if the error is a unique constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Common patterns for unique constraint violations
	return containsAny(errStr,
		"UNIQUE constraint failed",
		"duplicate key",
		"Duplicate entry",
		"unique_violation",
		"23505", // PostgreSQL error code
	)
}

func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
