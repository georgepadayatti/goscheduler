// Web server example that schedules jobs via HTTP API calls.
// Uses PostgreSQL job store for persistence.
//
// Run:
//
//	POSTGRES_DSN="host=localhost port=5432 user=postgres password=postgres dbname=goscheduler sslmode=disable" \
//	  go run ./examples/webserver
//
// Flags:
//
//	-postgres "<dsn>"  PostgreSQL connection string
//	-table "name"      Table name for stored jobs
//
// Add an interval job (every 5s):
//
//	curl -X POST http://localhost:8080/jobs \
//	  -H "Content-Type: application/json" \
//	  -d '{"message":"hello","trigger":{"type":"interval","interval_seconds":5}}'
//
// Add a one-time job:
//
//	curl -X POST http://localhost:8080/jobs \
//	  -H "Content-Type: application/json" \
//	  -d '{"message":"run once","trigger":{"type":"date","run_at":"2026-01-03T12:34:56Z"}}'
//
// List jobs:
//
//	curl http://localhost:8080/jobs
//
// Pause/resume/delete:
//
//	curl -X POST http://localhost:8080/jobs/<id>/pause
//	curl -X POST http://localhost:8080/jobs/<id>/resume
//	curl -X DELETE http://localhost:8080/jobs/<id>
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/georgejpadayatti/goscheduler/job"
	"github.com/georgejpadayatti/goscheduler/jobstore"
	"github.com/georgejpadayatti/goscheduler/scheduler"
	"github.com/georgejpadayatti/goscheduler/trigger"
)

type addJobRequest struct {
	ID                  string      `json:"id,omitempty"`
	Name                string      `json:"name,omitempty"`
	Function            string      `json:"function,omitempty"`
	Message             string      `json:"message,omitempty"`
	Trigger             triggerSpec `json:"trigger"`
	Coalesce            *bool       `json:"coalesce,omitempty"`
	MisfireGraceSeconds *int        `json:"misfire_grace_seconds,omitempty"`
	MaxInstances        *int        `json:"max_instances,omitempty"`
}

type triggerSpec struct {
	Type            string `json:"type"`
	IntervalSeconds int    `json:"interval_seconds,omitempty"`
	RunAt           string `json:"run_at,omitempty"`
}

type jobResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Executor    string     `json:"executor"`
	JobStore    string     `json:"job_store"`
	Trigger     string     `json:"trigger"`
	NextRunTime *time.Time `json:"next_run_time,omitempty"`
	Paused      bool       `json:"paused"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type apiServer struct {
	sched     scheduler.Scheduler
	functions map[string]interface{}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	dsnDefault := envOrDefault("POSTGRES_DSN", "host=localhost port=5432 user=postgres password=postgres dbname=goscheduler sslmode=disable")
	dsn := flag.String("postgres", dsnDefault, "PostgreSQL connection string")
	tableName := flag.String("table", "webserver_jobs", "table name for stored jobs")
	flag.Parse()

	store, err := jobstore.NewPostgreSQLJobStore(jobstore.PostgreSQLJobStoreConfig{
		ConnString: *dsn,
		TableName:  *tableName,
	})
	if err != nil {
		log.Fatalf("postgres job store init failed: %v", err)
	}

	sched := scheduler.NewBackgroundScheduler(scheduler.DefaultConfig())
	if err := sched.AddJobStore(store, "default"); err != nil {
		log.Fatalf("failed to add postgres job store: %v", err)
	}
	functions := registerJobFunctions()

	server := &apiServer{
		sched:     sched,
		functions: functions,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := sched.Start(ctx); err != nil {
		log.Fatalf("scheduler start failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/jobs", server.handleJobs)
	mux.HandleFunc("/jobs/", server.handleJob)

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		_ = sched.Shutdown(true)
	}()

	log.Println("HTTP API listening on :8080")
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server error: %v", err)
	}

	_ = sched.Shutdown(true)
	log.Println("shutdown complete")
}

func registerJobFunctions() map[string]interface{} {
	functions := map[string]interface{}{
		"print": printMessage,
	}

	for _, fn := range functions {
		if _, err := job.RegisterFuncByName(fn); err != nil {
			log.Printf("job function registration failed: %v", err)
		}
	}

	return functions
}

func printMessage(message string) {
	log.Printf("job message: %s", message)
}

func (s *apiServer) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListJobs(w, r)
	case http.MethodPost:
		s.handleCreateJob(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *apiServer) handleJob(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/jobs/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	jobID := parts[0]

	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleGetJob(w, r, jobID)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.handleDeleteJob(w, r, jobID)
	case len(parts) == 2 && r.Method == http.MethodPost && parts[1] == "pause":
		s.handlePauseJob(w, r, jobID)
	case len(parts) == 2 && r.Method == http.MethodPost && parts[1] == "resume":
		s.handleResumeJob(w, r, jobID)
	default:
		writeError(w, http.StatusNotFound, "endpoint not found")
	}
}

func (s *apiServer) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req addJobRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Function == "" {
		req.Function = "print"
	}

	fn, ok := s.functions[req.Function]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown function %q", req.Function))
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	trig, err := parseTrigger(req.Trigger)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	opts := []job.Option{
		job.WithArgs(req.Message),
	}

	if req.ID != "" {
		opts = append(opts, job.WithID(req.ID))
	}
	if req.Name != "" {
		opts = append(opts, job.WithName(req.Name))
	}
	if req.Coalesce != nil {
		opts = append(opts, job.WithCoalesce(*req.Coalesce))
	}
	if req.MaxInstances != nil {
		opts = append(opts, job.WithMaxInstances(*req.MaxInstances))
	}
	if req.MisfireGraceSeconds != nil {
		if *req.MisfireGraceSeconds < 0 {
			writeError(w, http.StatusBadRequest, "misfire_grace_seconds must be >= 0")
			return
		}
		if *req.MisfireGraceSeconds == 0 {
			opts = append(opts, job.WithNoMisfireGraceTime())
		} else {
			opts = append(opts, job.WithMisfireGraceTime(time.Duration(*req.MisfireGraceSeconds)*time.Second))
		}
	}

	newJob, err := s.sched.AddJob(fn, trig, opts...)
	if err != nil {
		if _, ok := err.(*jobstore.ConflictingIDError); ok {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toJobResponse(newJob))
}

func (s *apiServer) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.sched.GetJobs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]jobResponse, 0, len(jobs))
	for _, j := range jobs {
		resp = append(resp, toJobResponse(j))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *apiServer) handleGetJob(w http.ResponseWriter, r *http.Request, jobID string) {
	j, err := s.sched.GetJob(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if j == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	writeJSON(w, http.StatusOK, toJobResponse(j))
}

func (s *apiServer) handleDeleteJob(w http.ResponseWriter, r *http.Request, jobID string) {
	j, err := s.sched.GetJob(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if j == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	if err := s.sched.RemoveJob(jobID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *apiServer) handlePauseJob(w http.ResponseWriter, r *http.Request, jobID string) {
	j, err := s.sched.GetJob(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if j == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	if _, err := s.sched.PauseJob(jobID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *apiServer) handleResumeJob(w http.ResponseWriter, r *http.Request, jobID string) {
	j, err := s.sched.GetJob(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if j == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	if _, err := s.sched.ResumeJob(jobID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func parseTrigger(spec triggerSpec) (trigger.Trigger, error) {
	switch strings.ToLower(spec.Type) {
	case "interval":
		if spec.IntervalSeconds <= 0 {
			return nil, fmt.Errorf("interval_seconds must be > 0")
		}
		return trigger.NewIntervalTrigger(time.Duration(spec.IntervalSeconds) * time.Second), nil
	case "date":
		if spec.RunAt == "" {
			return nil, fmt.Errorf("run_at is required for date trigger")
		}
		runAt, err := time.Parse(time.RFC3339, spec.RunAt)
		if err != nil {
			return nil, fmt.Errorf("invalid run_at: %w", err)
		}
		return trigger.NewDateTrigger(runAt), nil
	default:
		if spec.Type == "" {
			return nil, fmt.Errorf("trigger.type is required")
		}
		return nil, fmt.Errorf("unsupported trigger type %q", spec.Type)
	}
}

func toJobResponse(j *job.Job) jobResponse {
	if j == nil {
		return jobResponse{}
	}

	snapshot := j.Clone()
	return jobResponse{
		ID:          snapshot.ID,
		Name:        snapshot.Name,
		Executor:    snapshot.Executor,
		JobStore:    snapshot.JobStore,
		Trigger:     safeTriggerString(snapshot.Trigger),
		NextRunTime: snapshot.GetNextRunTime(),
		Paused:      snapshot.IsPaused(),
	}
}

func safeTriggerString(value interface{}) string {
	if isNilInterface(value) {
		return ""
	}
	if str, ok := value.(interface{ String() string }); ok {
		return str.String()
	}
	return ""
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

func decodeJSON(r *http.Request, dst interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("invalid JSON: multiple values")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
