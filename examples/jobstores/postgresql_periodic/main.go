// Demonstrates a periodic job with external dependencies (PostgreSQL database).
// The job connects to PostgreSQL, creates a table if it doesn't exist,
// and adds an entry every 10 seconds.
//
// Usage:
//
//	go run main.go
//	go run main.go --clear  # Clear all existing jobs
//
// Default connection: host=localhost port=5432 user=postgres password=postgres dbname=goscheduler sslmode=disable
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/georgejpadayatti/goscheduler/event"
	"github.com/georgejpadayatti/goscheduler/job"
	"github.com/georgejpadayatti/goscheduler/jobstore"
	"github.com/georgejpadayatti/goscheduler/scheduler"
	"github.com/georgejpadayatti/goscheduler/trigger"

	_ "github.com/lib/pq"
)

// Global database connection for the job to use
var db *sql.DB

// recordEntry is the job function that connects to the database,
// ensures the table exists, and adds a new entry.
func recordEntry() error {
	// Create table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS job_entries (
			id SERIAL PRIMARY KEY,
			message TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Insert a new entry
	message := fmt.Sprintf("Job executed at %s", time.Now().Format("2006-01-02 15:04:05"))
	_, err = db.Exec(`INSERT INTO job_entries (message) VALUES ($1)`, message)
	if err != nil {
		return fmt.Errorf("failed to insert entry: %w", err)
	}

	// Count total entries
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM job_entries`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count entries: %w", err)
	}

	fmt.Printf("[%s] Recorded entry. Total entries: %d\n",
		time.Now().Format("15:04:05"), count)

	return nil
}

func init() {
	// Register the job function so it can be restored from persistent storage
	if _, err := job.RegisterFuncByName(recordEntry); err != nil {
		_ = err
	}
}

func main() {
	// Default PostgreSQL connection string
	connString := "host=localhost port=5432 user=postgres password=postgres dbname=goscheduler sslmode=disable"

	clearJobs := false
	if len(os.Args) > 1 {
		if os.Args[1] == "--clear" {
			clearJobs = true
		} else {
			connString = os.Args[1]
		}
	}

	// Open database connection for the job to use
	var err error
	db, err = sql.Open("postgres", connString)
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		return
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		fmt.Printf("Error connecting to database: %v\n", err)
		return
	}
	fmt.Println("Connected to PostgreSQL database")

	// Create PostgreSQL job store (uses the same database)
	store, err := jobstore.NewPostgreSQLJobStore(jobstore.PostgreSQLJobStoreConfig{
		ConnString: connString,
		TableName:  "periodic_jobs",
	})
	if err != nil {
		fmt.Printf("Error creating job store: %v\n", err)
		return
	}

	// Create scheduler
	sched := scheduler.NewBlockingScheduler(scheduler.DefaultConfig())

	// Add the PostgreSQL job store
	if err := sched.AddJobStore(store, "default"); err != nil {
		fmt.Printf("Error adding job store: %v\n", err)
		return
	}

	// Add event listener for job execution
	sched.AddListener(func(e event.Event) {
		switch evt := e.(type) {
		case *event.JobExecutionEvent:
			if evt.Exception != nil {
				fmt.Printf("[Event] Job %s error: %v\n", evt.JobID, evt.Exception)
			}
		}
	}, event.JobExecuted|event.JobError)

	// Clear jobs if requested
	if clearJobs {
		fmt.Println("Clearing all existing jobs...")
		if err := sched.RemoveAllJobs(); err != nil {
			fmt.Printf("Error clearing jobs: %v\n", err)
		} else {
			fmt.Println("All jobs cleared.")
		}
		// Also clear the entries table
		_, _ = db.Exec(`DROP TABLE IF EXISTS job_entries`)
		fmt.Println("Dropped job_entries table.")
		return
	}

	// Try to add the job - it will fail with ConflictingIDError if it already exists
	_, err = sched.AddJob(
		recordEntry,
		trigger.NewIntervalTrigger(10*time.Second),
		job.WithID("periodic-db-job"),
		job.WithName("Periodic Database Entry Job"),
	)
	if err != nil {
		// Check if job already exists (ConflictingIDError means it's persisted from a previous run)
		if _, ok := err.(*jobstore.ConflictingIDError); ok {
			fmt.Println("Found existing periodic job from previous run, resuming...")
		} else {
			fmt.Printf("Error adding job: %v\n", err)
			return
		}
	} else {
		fmt.Println("Added new periodic job (runs every 10 seconds)")
	}

	fmt.Println("To clear the jobs and entries, run with the --clear argument.")
	fmt.Println("Press Ctrl+C to exit")
	fmt.Println("---")

	// Handle shutdown gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		sched.Shutdown(true)
		cancel()
	}()

	// Start the scheduler
	if err := sched.Start(ctx); err != nil {
		fmt.Printf("Scheduler error: %v\n", err)
	}
}
