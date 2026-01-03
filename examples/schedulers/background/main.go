// Demonstrates how to use the background scheduler to schedule a job that executes
// on 3 second intervals.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/georgepadayatti/goscheduler/job"
	"github.com/georgepadayatti/goscheduler/scheduler"
	"github.com/georgepadayatti/goscheduler/trigger"
)

func tick() {
	fmt.Printf("Tick! The time is: %s\n", time.Now().Format("2006-01-02 15:04:05"))
}

func main() {
	sched := scheduler.NewBackgroundScheduler(scheduler.DefaultConfig())

	_, err := sched.AddJob(
		tick,
		trigger.NewIntervalTrigger(3*time.Second),
		job.WithID("tick-job"),
		job.WithName("Tick Job"),
	)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}

	// Start the scheduler in the background
	if err := sched.Start(context.Background()); err != nil {
		fmt.Printf("Error starting scheduler: %v\n", err)
		return
	}

	fmt.Println("Scheduler started in background")
	fmt.Println("Press Ctrl+C to exit")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Simulate application activity
	go func() {
		for {
			time.Sleep(2 * time.Second)
		}
	}()

	<-sigCh
	fmt.Println("\nShutting down scheduler...")
	sched.Shutdown(true)
	fmt.Println("Scheduler stopped.")
}
