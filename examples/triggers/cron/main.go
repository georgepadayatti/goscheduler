// Demonstrates the use of cron triggers.
// Shows various cron expression patterns and options.
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
	"github.com/georgejpadayatti/goscheduler/scheduler"
	"github.com/georgejpadayatti/goscheduler/trigger/cron"
)

func everyMinute() {
	fmt.Printf("[%s] Every minute job executed\n", time.Now().Format("15:04:05"))
}

func weekdaysAt9AM() {
	fmt.Printf("[%s] Weekdays at 9 AM job executed\n", time.Now().Format("15:04:05"))
}

func every5Seconds() {
	fmt.Printf("[%s] Every 5 seconds job executed\n", time.Now().Format("15:04:05"))
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

	// Job 1: Every minute at the start of the minute
	everyMinuteTrigger, err := cron.NewCronTrigger(
		cron.WithSecond("0"),
		cron.WithCronLocation(time.Local),
	)
	if err != nil {
		fmt.Printf("Error creating cron trigger: %v\n", err)
		return
	}

	_, err = sched.AddJob(
		everyMinute,
		everyMinuteTrigger,
		job.WithID("every-minute"),
		job.WithName("Every Minute Job"),
	)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}
	fmt.Println("Added job: Every minute at second 0")

	// Job 2: Every 5 seconds (using */5 for seconds)
	every5SecTrigger, err := cron.NewCronTrigger(
		cron.WithSecond("*/5"),
		cron.WithCronLocation(time.Local),
	)
	if err != nil {
		fmt.Printf("Error creating cron trigger: %v\n", err)
		return
	}

	_, err = sched.AddJob(
		every5Seconds,
		every5SecTrigger,
		job.WithID("every-5-seconds"),
		job.WithName("Every 5 Seconds Job"),
	)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}
	fmt.Println("Added job: Every 5 seconds")

	// Job 3: Weekdays at 9 AM (using crontab format)
	weekdaysTrigger, err := cron.FromCrontab("0 9 * * mon-fri", time.Local)
	if err != nil {
		fmt.Printf("Error creating cron trigger: %v\n", err)
		return
	}

	_, err = sched.AddJob(
		weekdaysAt9AM,
		weekdaysTrigger,
		job.WithID("weekdays-9am"),
		job.WithName("Weekdays at 9 AM"),
	)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}
	fmt.Println("Added job: Weekdays at 9 AM")

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
