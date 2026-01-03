// Demonstrates the use of combining triggers (And/Or).
// Shows how to combine multiple triggers to create complex scheduling patterns.
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
	"github.com/georgejpadayatti/goscheduler/trigger"
	"github.com/georgejpadayatti/goscheduler/trigger/cron"
)

func orTriggerJob() {
	fmt.Printf("[%s] OR trigger job executed\n", time.Now().Format("15:04:05"))
}

func andTriggerJob() {
	fmt.Printf("[%s] AND trigger job executed\n", time.Now().Format("15:04:05"))
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

	// Example 1: OR trigger - fires when EITHER trigger condition is met
	// This creates a schedule that fires every 5 seconds OR every 7 seconds
	orTrig := trigger.NewOrTrigger(
		[]trigger.Trigger{
			trigger.NewIntervalTrigger(5 * time.Second),
			trigger.NewIntervalTrigger(7 * time.Second),
		},
		0, // No jitter
	)
	_, err := sched.AddJob(
		orTriggerJob,
		orTrig,
		job.WithID("or-trigger"),
		job.WithName("OR Trigger (5s OR 7s)"),
	)
	if err != nil {
		fmt.Printf("Error adding OR trigger job: %v\n", err)
		return
	}
	fmt.Println("Added job: OR trigger (fires every 5 seconds OR every 7 seconds)")

	// Example 2: AND trigger - fires when ALL trigger conditions are met
	// This combines a cron expression with an interval constraint
	// Only fires when cron condition matches AND interval has passed
	cronTrig, err := cron.NewCronTrigger(
		cron.WithSecond("*/10"), // Every 10 seconds based on clock
		cron.WithCronLocation(time.Local),
	)
	if err != nil {
		fmt.Printf("Error creating cron trigger: %v\n", err)
		return
	}

	andTrig := trigger.NewAndTrigger(
		[]trigger.Trigger{
			cronTrig,
			trigger.NewIntervalTrigger(15 * time.Second), // With 15 second minimum interval
		},
		0, // No jitter
	)
	_, err = sched.AddJob(
		andTriggerJob,
		andTrig,
		job.WithID("and-trigger"),
		job.WithName("AND Trigger (cron AND interval)"),
	)
	if err != nil {
		fmt.Printf("Error adding AND trigger job: %v\n", err)
		return
	}
	fmt.Println("Added job: AND trigger (cron every 10s clock-aligned AND minimum 15s interval)")

	// Example 3: Complex combining - OR of multiple cron triggers
	// Useful for business hours scenarios
	morning, _ := cron.NewCronTrigger(
		cron.WithSecond("0"),
		cron.WithMinute("0"),
		cron.WithHour("9"),
		cron.WithCronLocation(time.Local),
	)
	afternoon, _ := cron.NewCronTrigger(
		cron.WithSecond("0"),
		cron.WithMinute("0"),
		cron.WithHour("14"),
		cron.WithCronLocation(time.Local),
	)
	evening, _ := cron.NewCronTrigger(
		cron.WithSecond("0"),
		cron.WithMinute("0"),
		cron.WithHour("18"),
		cron.WithCronLocation(time.Local),
	)

	businessHoursTrig := trigger.NewOrTrigger(
		[]trigger.Trigger{morning, afternoon, evening},
		0, // No jitter
	)
	_, err = sched.AddJob(
		func() {
			fmt.Printf("[%s] Business hours reminder!\n", time.Now().Format("15:04:05"))
		},
		businessHoursTrig,
		job.WithID("business-hours"),
		job.WithName("Business Hours Reminder (9am, 2pm, 6pm)"),
	)
	if err != nil {
		fmt.Printf("Error adding business hours job: %v\n", err)
		return
	}
	fmt.Println("Added job: Business hours reminder (fires at 9am, 2pm, and 6pm)")

	// Start the scheduler
	if err := sched.Start(context.Background()); err != nil {
		fmt.Printf("Error starting scheduler: %v\n", err)
		return
	}

	fmt.Println()
	fmt.Println("Scheduler started. Press Ctrl+C to exit")
	fmt.Println("Watch for the OR trigger to fire at different intervals!")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	sched.Shutdown(true)
}
