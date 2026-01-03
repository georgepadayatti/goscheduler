// Demonstrates how to use the blocking scheduler to schedule a job that executes
// on 3 second intervals.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/georgejpadayatti/goscheduler/job"
	"github.com/georgejpadayatti/goscheduler/scheduler"
	"github.com/georgejpadayatti/goscheduler/trigger"
)

func tick() {
	fmt.Printf("Tick! The time is: %s\n", time.Now().Format("2006-01-02 15:04:05"))
}

func main() {
	sched := scheduler.NewBlockingScheduler(scheduler.DefaultConfig())

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

	// Start blocks until shutdown
	if err := sched.Start(ctx); err != nil {
		fmt.Printf("Scheduler error: %v\n", err)
	}
}
