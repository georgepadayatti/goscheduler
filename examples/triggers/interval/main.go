// Demonstrates the use of interval triggers.
// Shows various interval configurations including jitter and start/end dates.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/georgepadayatti/goscheduler/event"
	"github.com/georgepadayatti/goscheduler/job"
	"github.com/georgepadayatti/goscheduler/scheduler"
	"github.com/georgepadayatti/goscheduler/trigger"
)

func every3Seconds() {
	fmt.Printf("[%s] Every 3 seconds job executed\n", time.Now().Format("15:04:05"))
}

func every10SecondsWithJitter() {
	fmt.Printf("[%s] Every 10 seconds with jitter job executed\n", time.Now().Format("15:04:05"))
}

func main() {
	sched := scheduler.NewBackgroundScheduler(scheduler.DefaultConfig())

	// Add event listener
	sched.AddListener(func(e event.Event) {
		switch evt := e.(type) {
		case *event.JobExecutionEvent:
			fmt.Printf("[Event] Job %s executed\n", evt.JobID)
		}
	}, event.JobExecuted)

	// Job 1: Simple interval - every 3 seconds
	_, err := sched.AddJob(
		every3Seconds,
		trigger.NewIntervalTrigger(3*time.Second),
		job.WithID("every-3-seconds"),
		job.WithName("Every 3 Seconds Job"),
	)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}
	fmt.Println("Added job: Every 3 seconds")

	// Job 2: Interval with jitter (adds randomness to prevent thundering herd)
	trigWithJitter := trigger.NewIntervalTrigger(
		10*time.Second,
		trigger.WithIntervalJitter(2*time.Second), // +/- 2 seconds jitter
	)
	_, err = sched.AddJob(
		every10SecondsWithJitter,
		trigWithJitter,
		job.WithID("every-10-seconds-jitter"),
		job.WithName("Every 10 Seconds with Jitter"),
	)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}
	fmt.Println("Added job: Every 10 seconds with +/- 2 seconds jitter")

	// Job 3: Interval with start and end date
	startDate := time.Now().Add(5 * time.Second)
	endDate := time.Now().Add(30 * time.Second)

	trigWithDates := trigger.NewIntervalTrigger(
		5*time.Second,
		trigger.WithIntervalStartDate(startDate),
		trigger.WithIntervalEndDate(endDate),
	)
	_, err = sched.AddJob(
		func() {
			fmt.Printf("[%s] Limited time job executed\n", time.Now().Format("15:04:05"))
		},
		trigWithDates,
		job.WithID("limited-time"),
		job.WithName("Limited Time Job (runs for 30 seconds)"),
	)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}
	fmt.Printf("Added job: Every 5 seconds, starts in 5 seconds, ends at %s\n", endDate.Format("15:04:05"))

	// Start the scheduler
	if err := sched.Start(context.Background()); err != nil {
		fmt.Printf("Error starting scheduler: %v\n", err)
		return
	}

	fmt.Println()
	fmt.Println("Scheduler started. Press Ctrl+C to exit")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	sched.Shutdown(true)
}
