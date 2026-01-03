package jobstore

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/georgepadayatti/goscheduler/job"
	jobtrigger "github.com/georgepadayatti/goscheduler/trigger"

	// PostgreSQL driver
	_ "github.com/lib/pq"
)

// PostgreSQLJobStore stores jobs in a PostgreSQL database.
// Optimized for PostgreSQL with proper SQL syntax and features.
type PostgreSQLJobStore struct {
	db        *sql.DB
	tableName string
	schema    string
	scheduler any
	alias     string
	ownsDB    bool // true if we created the connection
}

// PostgreSQLJobStoreConfig configures the PostgreSQL job store.
type PostgreSQLJobStoreConfig struct {
	// Existing database connection (takes precedence)
	DB *sql.DB

	// Connection string (used if DB is nil)
	// e.g., "host=localhost port=5432 user=postgres password=secret dbname=mydb sslmode=disable"
	ConnString string

	// Table configuration
	Schema    string // default: "public"
	TableName string // default: "goscheduler_jobs"
}

// NewPostgreSQLJobStore creates a new PostgreSQL job store.
func NewPostgreSQLJobStore(cfg PostgreSQLJobStoreConfig) (*PostgreSQLJobStore, error) {
	var db *sql.DB
	var err error
	var ownsDB bool

	if cfg.DB != nil {
		db = cfg.DB
		ownsDB = false
	} else if cfg.ConnString != "" {
		db, err = sql.Open("postgres", cfg.ConnString)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		ownsDB = true
	} else {
		return nil, fmt.Errorf("either DB or ConnString must be provided")
	}

	schema := cfg.Schema
	if schema == "" {
		schema = "public"
	}

	tableName := cfg.TableName
	if tableName == "" {
		tableName = "goscheduler_jobs"
	}

	return &PostgreSQLJobStore{
		db:        db,
		tableName: tableName,
		schema:    schema,
		ownsDB:    ownsDB,
	}, nil
}

// fullTableName returns the fully qualified table name.
func (s *PostgreSQLJobStore) fullTableName() string {
	return fmt.Sprintf("%s.%s", s.schema, s.tableName)
}

// Start initializes the job store and creates the table if needed.
func (s *PostgreSQLJobStore) Start(scheduler any, alias string) error {
	s.scheduler = scheduler
	s.alias = alias

	// Create schema if not exists
	_, err := s.db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", s.schema))
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Create table with PostgreSQL-specific types
	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(191) PRIMARY KEY,
			next_run_time DOUBLE PRECISION,
			job_state BYTEA NOT NULL
		)
	`, s.fullTableName()))
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Create index on next_run_time
	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_next_run_time
		ON %s (next_run_time)
		WHERE next_run_time IS NOT NULL
	`, s.tableName, s.fullTableName()))
	if err != nil {
		// Index might already exist with different name, ignore
	}

	return nil
}

// Shutdown closes the database connection if we own it.
func (s *PostgreSQLJobStore) Shutdown() error {
	if s.ownsDB && s.db != nil {
		return s.db.Close()
	}
	return nil
}

// LookupJob returns a job by ID.
func (s *PostgreSQLJobStore) LookupJob(jobID string) (*job.Job, error) {
	row := s.db.QueryRow(
		fmt.Sprintf("SELECT job_state FROM %s WHERE id = $1", s.fullTableName()),
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
func (s *PostgreSQLJobStore) GetDueJobs(now time.Time) ([]*job.Job, error) {
	nowTimestamp := float64(now.UTC().Unix()) + float64(now.Nanosecond())/1e9

	rows, err := s.db.Query(
		fmt.Sprintf(`
			SELECT job_state FROM %s
			WHERE next_run_time IS NOT NULL AND next_run_time <= $1
			ORDER BY next_run_time ASC
		`, s.fullTableName()),
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
func (s *PostgreSQLJobStore) GetNextRunTime() *time.Time {
	row := s.db.QueryRow(
		fmt.Sprintf(`
			SELECT next_run_time FROM %s
			WHERE next_run_time IS NOT NULL
			ORDER BY next_run_time ASC
			LIMIT 1
		`, s.fullTableName()),
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
func (s *PostgreSQLJobStore) GetAllJobs() ([]*job.Job, error) {
	rows, err := s.db.Query(
		fmt.Sprintf(`
			SELECT job_state FROM %s
			ORDER BY next_run_time ASC NULLS LAST, id ASC
		`, s.fullTableName()),
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
func (s *PostgreSQLJobStore) AddJob(j *job.Job) error {
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
			VALUES ($1, $2, $3)
		`, s.fullTableName()),
		j.ID, timestamp, stateData,
	)

	if err != nil {
		// Check for unique constraint violation (PostgreSQL error code 23505)
		if isPostgresUniqueViolation(err) {
			return &ConflictingIDError{JobID: j.ID}
		}
		return fmt.Errorf("failed to add job: %w", err)
	}

	return nil
}

// UpdateJob updates an existing job.
func (s *PostgreSQLJobStore) UpdateJob(j *job.Job) error {
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
			SET next_run_time = $1, job_state = $2
			WHERE id = $3
		`, s.fullTableName()),
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
func (s *PostgreSQLJobStore) RemoveJob(jobID string) error {
	result, err := s.db.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.fullTableName()),
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
func (s *PostgreSQLJobStore) RemoveAllJobs() error {
	_, err := s.db.Exec(fmt.Sprintf("DELETE FROM %s", s.fullTableName()))
	if err != nil {
		return fmt.Errorf("failed to remove all jobs: %w", err)
	}
	return nil
}

// serializeJob serializes a job to bytes.
func (s *PostgreSQLJobStore) serializeJob(j *job.Job) ([]byte, error) {
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
func (s *PostgreSQLJobStore) deserializeJob(data []byte) (*job.Job, error) {
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

// isPostgresUniqueViolation checks if the error is a PostgreSQL unique constraint violation.
func isPostgresUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsAny(errStr, "23505", "duplicate key", "unique constraint")
}
