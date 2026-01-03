package jobstore

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/georgejpadayatti/goscheduler/job"
	jobtrigger "github.com/georgejpadayatti/goscheduler/trigger"

	// MySQL driver
	_ "github.com/go-sql-driver/mysql"
)

// MySQLJobStore stores jobs in a MySQL database.
// Optimized for MySQL with proper SQL syntax and features.
type MySQLJobStore struct {
	db        *sql.DB
	tableName string
	scheduler any
	alias     string
	ownsDB    bool // true if we created the connection
}

// MySQLJobStoreConfig configures the MySQL job store.
type MySQLJobStoreConfig struct {
	// Existing database connection (takes precedence)
	DB *sql.DB

	// Data Source Name (used if DB is nil)
	// e.g., "user:password@tcp(localhost:3306)/dbname?parseTime=true"
	DSN string

	// Table name (default: "goscheduler_jobs")
	TableName string
}

// NewMySQLJobStore creates a new MySQL job store.
func NewMySQLJobStore(cfg MySQLJobStoreConfig) (*MySQLJobStore, error) {
	var db *sql.DB
	var err error
	var ownsDB bool

	if cfg.DB != nil {
		db = cfg.DB
		ownsDB = false
	} else if cfg.DSN != "" {
		db, err = sql.Open("mysql", cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		ownsDB = true
	} else {
		return nil, fmt.Errorf("either DB or DSN must be provided")
	}

	tableName := cfg.TableName
	if tableName == "" {
		tableName = "goscheduler_jobs"
	}

	return &MySQLJobStore{
		db:        db,
		tableName: tableName,
		ownsDB:    ownsDB,
	}, nil
}

// Start initializes the job store and creates the table if needed.
func (s *MySQLJobStore) Start(scheduler any, alias string) error {
	s.scheduler = scheduler
	s.alias = alias

	// Create table with MySQL-specific types
	_, err := s.db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(191) PRIMARY KEY,
			next_run_time DOUBLE,
			job_state LONGBLOB NOT NULL,
			INDEX idx_next_run_time (next_run_time)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`, s.tableName))
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// Shutdown closes the database connection if we own it.
func (s *MySQLJobStore) Shutdown() error {
	if s.ownsDB && s.db != nil {
		return s.db.Close()
	}
	return nil
}

// LookupJob returns a job by ID.
func (s *MySQLJobStore) LookupJob(jobID string) (*job.Job, error) {
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
func (s *MySQLJobStore) GetDueJobs(now time.Time) ([]*job.Job, error) {
	nowTimestamp := float64(now.UTC().Unix()) + float64(now.Nanosecond())/1e9

	rows, err := s.db.Query(
		fmt.Sprintf(`
			SELECT job_state FROM %s
			WHERE next_run_time IS NOT NULL AND next_run_time <= ?
			ORDER BY next_run_time ASC
		`, s.tableName),
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
			continue // Skip corrupted jobs
		}
		jobs = append(jobs, j)
	}

	return jobs, rows.Err()
}

// GetNextRunTime returns the earliest next run time.
func (s *MySQLJobStore) GetNextRunTime() *time.Time {
	row := s.db.QueryRow(
		fmt.Sprintf(`
			SELECT next_run_time FROM %s
			WHERE next_run_time IS NOT NULL
			ORDER BY next_run_time ASC
			LIMIT 1
		`, s.tableName),
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

// GetAllJobs returns all jobs sorted by next run time.
func (s *MySQLJobStore) GetAllJobs() ([]*job.Job, error) {
	// MySQL doesn't have NULLS LAST, so use CASE expression
	rows, err := s.db.Query(
		fmt.Sprintf(`
			SELECT job_state FROM %s
			ORDER BY CASE WHEN next_run_time IS NULL THEN 1 ELSE 0 END,
			         next_run_time ASC, id ASC
		`, s.tableName),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get all jobs: %w", err)
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
			continue
		}
		jobs = append(jobs, j)
	}

	return jobs, rows.Err()
}

// AddJob adds a job to the store.
func (s *MySQLJobStore) AddJob(j *job.Job) error {
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
		fmt.Sprintf(`
			INSERT INTO %s (id, next_run_time, job_state)
			VALUES (?, ?, ?)
		`, s.tableName),
		j.ID, timestamp, stateData,
	)

	if err != nil {
		// Check for duplicate key error (MySQL error code 1062)
		if isMySQLDuplicateKey(err) {
			return &ConflictingIDError{JobID: j.ID}
		}
		return fmt.Errorf("failed to add job: %w", err)
	}

	return nil
}

// UpdateJob updates an existing job.
func (s *MySQLJobStore) UpdateJob(j *job.Job) error {
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
		fmt.Sprintf(`
			UPDATE %s
			SET next_run_time = ?, job_state = ?
			WHERE id = ?
		`, s.tableName),
		timestamp, stateData, j.ID,
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
func (s *MySQLJobStore) RemoveJob(jobID string) error {
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
func (s *MySQLJobStore) RemoveAllJobs() error {
	_, err := s.db.Exec(fmt.Sprintf("DELETE FROM %s", s.tableName))
	if err != nil {
		return fmt.Errorf("failed to remove all jobs: %w", err)
	}
	return nil
}

// serializeJob serializes a job to bytes.
func (s *MySQLJobStore) serializeJob(j *job.Job) ([]byte, error) {
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
func (s *MySQLJobStore) deserializeJob(data []byte) (*job.Job, error) {
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

// isMySQLDuplicateKey checks if the error is a MySQL duplicate key error.
func isMySQLDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsAny(errStr, "1062", "Duplicate entry", "duplicate key")
}
