// Demonstrates the use of the MySQL job store.
// On each run, it adds a new alarm that fires after ten seconds.
// You can exit the program, restart it and observe that any previous alarms
// that have not fired yet are still active.
//
// Usage:
//   go run main.go [dsn]
//   go run main.go --clear  # Clear all existing alarms
//
// Default MySQL DSN: root:password@tcp(localhost:3306)/goscheduler?parseTime=true
package main

import (
	"context"
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
)

func alarm(scheduledAt string) {
	fmt.Printf("Alarm! This alarm was scheduled at %s\n", scheduledAt)
}

func init() {
	// Register the alarm function so it can be restored from persistent storage
	if _, err := job.RegisterFuncByName(alarm); err != nil {
		_ = err
	}
}

func main() {
	// Default MySQL DSN
	dsn := "root:password@tcp(localhost:3306)/goscheduler?parseTime=true"

	clearAlarms := false
	if len(os.Args) > 1 {
		if os.Args[1] == "--clear" {
			clearAlarms = true
		} else {
			dsn = os.Args[1]
		}
	}

	// Create MySQL job store
	store, err := jobstore.NewMySQLJobStore(jobstore.MySQLJobStoreConfig{
		DSN:       dsn,
		TableName: "example_jobs",
	})
	if err != nil {
		fmt.Printf("Error creating job store: %v\n", err)
		return
	}

	// Create scheduler and add MySQL job store
	sched := scheduler.NewBlockingScheduler(scheduler.DefaultConfig())

	// Add the MySQL job store
	if err := sched.AddJobStore(store, "default"); err != nil {
		fmt.Printf("Error adding job store: %v\n", err)
		return
	}

	// Add event listener
	sched.AddListener(func(e event.Event) {
		switch evt := e.(type) {
		case *event.JobExecutionEvent:
			if evt.Exception != nil {
				fmt.Printf("[Event] Job %s error: %v\n", evt.JobID, evt.Exception)
			} else {
				fmt.Printf("[Event] Job %s executed successfully\n", evt.JobID)
			}
		}
	}, event.JobExecuted|event.JobError)

	// Clear alarms if requested
	if clearAlarms {
		fmt.Println("Clearing all existing alarms...")
		if err := sched.RemoveAllJobs(); err != nil {
			fmt.Printf("Error clearing jobs: %v\n", err)
		} else {
			fmt.Println("All alarms cleared.")
		}
		return
	}

	// Schedule a new alarm for 10 seconds from now
	alarmTime := time.Now().Add(10 * time.Second)
	scheduledAt := time.Now().Format("2006-01-02 15:04:05")

	_, err = sched.AddJob(
		alarm,
		trigger.NewDateTrigger(alarmTime),
		job.WithID(fmt.Sprintf("alarm-%d", time.Now().UnixNano())),
		job.WithName("Alarm Job"),
		job.WithArgs(scheduledAt),
	)
	if err != nil {
		fmt.Printf("Error adding alarm: %v\n", err)
		return
	}

	fmt.Printf("Added alarm for %s\n", alarmTime.Format("2006-01-02 15:04:05"))
	fmt.Println("To clear the alarms, run this example with the --clear argument.")
	fmt.Println("Press Ctrl+C to exit")

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
