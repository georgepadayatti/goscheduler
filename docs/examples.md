# Examples

This document provides practical examples for common use cases.

## Basic Examples

### Simple Interval Job

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/georgepadayatti/goscheduler/job"
    "github.com/georgepadayatti/goscheduler/scheduler"
    "github.com/georgepadayatti/goscheduler/trigger"
)

func main() {
    sched := scheduler.NewBackgroundScheduler(scheduler.DefaultConfig())

    // Define job function
    sayHello := func() {
        fmt.Printf("[%s] Hello!\n", time.Now().Format(time.RFC3339))
    }

    // Add job that runs every 5 seconds
    sched.AddJob(sayHello, trigger.NewIntervalTrigger(5*time.Second))

    // Start scheduler
    ctx := context.Background()
    sched.Start(ctx)

    // Run for 30 seconds
    time.Sleep(30 * time.Second)
    sched.Shutdown(true)
}
```

### Job with Arguments

```go
greet := func(name string, times int) {
    for i := 0; i < times; i++ {
        fmt.Printf("Hello, %s!\n", name)
    }
}

sched.AddJob(
    greet,
    trigger.NewIntervalTrigger(10*time.Second),
    job.WithArgs("World", 3),
)
```

### One-Time Scheduled Job

```go
alarm := func(message string) {
    fmt.Printf("ALARM: %s\n", message)
}

// Schedule for 5 minutes from now
alarmTime := time.Now().Add(5 * time.Minute)
sched.AddJob(
    alarm,
    trigger.NewDateTrigger(alarmTime),
    job.WithArgs("Meeting starts!"),
)
```

## Cron Examples

### Daily Report

```go
import "github.com/georgepadayatti/goscheduler/trigger/cron"

generateReport := func() {
    fmt.Println("Generating daily report...")
}

// Run at 9 AM every day
cronTrig, _ := cron.FromCrontab("0 9 * * *", time.Local)
sched.AddJob(generateReport, cronTrig, job.WithName("Daily Report"))
```

### Weekday Processing

```go
// Run at 8:30 AM on weekdays (Monday-Friday)
cronTrig, _ := cron.FromCrontab("30 8 * * 1-5", time.Local)
sched.AddJob(processWorkday, cronTrig)
```

### Monthly Cleanup

```go
// Run at midnight on the first day of each month
cronTrig, _ := cron.NewCronTrigger(
    cron.WithSecond("0"),
    cron.WithMinute("0"),
    cron.WithHour("0"),
    cron.WithDay("1"),
    cron.WithCronLocation(time.Local),
)
sched.AddJob(monthlyCleanup, cronTrig)
```

## Event Handling

### Logging Job Execution

```go
import "github.com/georgepadayatti/goscheduler/event"

sched.AddListener(func(e event.Event) {
    switch evt := e.(type) {
    case *event.JobExecutionEvent:
        if evt.Exception != nil {
            fmt.Printf("ERROR: Job %s failed: %v\n", evt.JobID, evt.Exception)
        } else {
            fmt.Printf("SUCCESS: Job %s completed in %v\n",
                evt.JobID, evt.ReturnValue)
        }
    }
}, event.JobExecuted|event.JobError)
```

### Monitoring Scheduler Lifecycle

```go
sched.AddListener(func(e event.Event) {
    switch e.(type) {
    case *event.SchedulerEvent:
        fmt.Printf("Scheduler event: %v\n", e)
    }
}, event.SchedulerStarted|event.SchedulerShutdown|event.SchedulerPaused|event.SchedulerResumed)
```

### Error Recovery

```go
sched.AddListener(func(e event.Event) {
    if evt, ok := e.(*event.JobExecutionEvent); ok && evt.Exception != nil {
        // Log error
        log.Printf("Job %s failed: %v", evt.JobID, evt.Exception)

        // Send alert
        sendAlert(evt.JobID, evt.Exception)

        // Optionally reschedule
        if shouldRetry(evt.JobID) {
            retryTrigger := trigger.NewDateTrigger(time.Now().Add(5 * time.Minute))
            sched.RescheduleJob(evt.JobID, retryTrigger)
        }
    }
}, event.JobError)
```

## Persistent Jobs with Redis

```go
import "github.com/georgepadayatti/goscheduler/jobstore"

func main() {
    sched := scheduler.NewBackgroundScheduler(scheduler.DefaultConfig())

    // Setup Redis store
    store, _ := jobstore.NewRedisJobStore(jobstore.RedisJobStoreConfig{
        Addr:        "localhost:6379",
        JobsKey:     "myapp.jobs",
        RunTimesKey: "myapp.run_times",
    })
    sched.AddJobStore(store, "default")

    // Register function (required for restoration)
    processData := func(dataID string) {
        fmt.Printf("Processing: %s\n", dataID)
    }
    job.RegisterFuncByName(processData)

    // Add persistent job
    sched.AddJob(
        processData,
        trigger.NewIntervalTrigger(1*time.Minute),
        job.WithID("data-processor"),
        job.WithArgs("dataset-123"),
    )

    ctx := context.Background()
    sched.Start(ctx)

    // Job persists across restarts
    select {}
}
```

## HTTP API Scheduling

For a net/http server that creates jobs via API calls and persists them to PostgreSQL, see `examples/webserver/main.go`.

```bash
POSTGRES_DSN="host=localhost port=5432 user=postgres password=postgres dbname=goscheduler sslmode=disable" \
  go run ./examples/webserver

# Add an interval job (every 5s)
curl -X POST http://localhost:8080/jobs \
  -H "Content-Type: application/json" \
  -d '{"message":"hello","trigger":{"type":"interval","interval_seconds":5}}'

# List jobs
curl http://localhost:8080/jobs
```

## Job Management

### Pausing and Resuming Jobs

```go
// Add a job
job, _ := sched.AddJob(myFunc, trigger.NewIntervalTrigger(5*time.Second),
    job.WithID("my-job"))

// Pause the job
sched.PauseJob("my-job")
fmt.Println("Job paused")

time.Sleep(30 * time.Second)

// Resume the job
sched.ResumeJob("my-job")
fmt.Println("Job resumed")
```

### Rescheduling Jobs

```go
// Add a job with initial trigger
sched.AddJob(myFunc,
    trigger.NewIntervalTrigger(1*time.Minute),
    job.WithID("adjustable-job"))

// Later, reschedule with a new trigger
newTrigger := trigger.NewIntervalTrigger(30 * time.Second)
sched.RescheduleJob("adjustable-job", newTrigger)
```

### Listing All Jobs

```go
jobs, _ := sched.GetJobs()
for _, j := range jobs {
    fmt.Printf("Job: %s, Next Run: %v, Paused: %v\n",
        j.ID, j.NextRunTime, j.IsPaused())
}
```

## Concurrency Control

### Limiting Concurrent Instances

```go
// Only allow one instance at a time
sched.AddJob(
    longRunningTask,
    trigger.NewIntervalTrigger(10*time.Second),
    job.WithMaxInstances(1),  // Skip if already running
)

// Allow up to 3 concurrent instances
sched.AddJob(
    parallelTask,
    trigger.NewIntervalTrigger(5*time.Second),
    job.WithMaxInstances(3),
)
```

### Handling Max Instances Events

```go
sched.AddListener(func(e event.Event) {
    if evt, ok := e.(*event.JobExecutionEvent); ok {
        fmt.Printf("Job %s skipped: max instances reached\n", evt.JobID)
    }
}, event.JobMaxInstances)
```

## Graceful Shutdown

### With Signal Handling

```go
import (
    "os"
    "os/signal"
    "syscall"
)

func main() {
    sched := scheduler.NewBackgroundScheduler(scheduler.DefaultConfig())

    // Add jobs...

    ctx := context.Background()
    sched.Start(ctx)

    // Wait for shutdown signal
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan

    fmt.Println("Shutting down...")

    // wait=true: wait for running jobs to complete
    sched.Shutdown(true)

    fmt.Println("Shutdown complete")
}
```

### With Context Cancellation

```go
ctx, cancel := context.WithCancel(context.Background())

sched := scheduler.NewBackgroundScheduler(scheduler.DefaultConfig())
sched.Start(ctx)

// Later, cancel the context
cancel()

// Scheduler will shutdown
sched.Shutdown(true)
```

## Multiple Executors

```go
import "github.com/georgepadayatti/goscheduler/executor"

// Default executor for quick jobs
defaultExec := executor.NewGoroutinePoolExecutor(
    executor.WithMaxWorkers(10),
)
sched.AddExecutor(defaultExec, "default")

// Dedicated executor for heavy jobs
heavyExec := executor.NewGoroutinePoolExecutor(
    executor.WithMaxWorkers(5),
)
sched.AddExecutor(heavyExec, "heavy")

// Quick job uses default executor
sched.AddJob(quickTask, trigger.NewIntervalTrigger(1*time.Second))

// Heavy job uses dedicated executor
sched.AddJob(heavyTask,
    trigger.NewIntervalTrigger(1*time.Minute),
    job.WithExecutor("heavy"),
)
```

## Calendar-Based Scheduling

### Quarterly Reports

```go
// Run quarterly at 9 AM on the first day
trig := trigger.NewCalendarIntervalTrigger(
    trigger.WithCalendarMonths(3),
    trigger.WithCalendarHour(9),
    trigger.WithCalendarMinute(0),
)
sched.AddJob(quarterlyReport, trig)
```

### Weekly Backup

```go
// Every week at 2 AM
trig := trigger.NewCalendarIntervalTrigger(
    trigger.WithCalendarWeeks(1),
    trigger.WithCalendarHour(2),
    trigger.WithCalendarMinute(0),
)
sched.AddJob(weeklyBackup, trig)
```

## Combining Triggers

### Business Hours Only

```go
// Run every 5 minutes during business hours
intervalTrig := trigger.NewIntervalTrigger(5 * time.Minute)

// 9 AM to 5 PM, Monday through Friday
businessHours, _ := cron.NewCronTrigger(
    cron.WithHour("9-17"),
    cron.WithDayOfWeek("1-5"),
    cron.WithCronLocation(time.Local),
)

combinedTrig := trigger.NewAndTrigger(intervalTrig, businessHours)
sched.AddJob(businessTask, combinedTrig)
```

### Multiple Schedules

```go
// Run at either schedule
morningTrig, _ := cron.FromCrontab("0 9 * * *", time.Local)
eveningTrig, _ := cron.FromCrontab("0 17 * * *", time.Local)

combinedTrig := trigger.NewOrTrigger(morningTrig, eveningTrig)
sched.AddJob(twiceDaily, combinedTrig)
```

## Error Handling

### Job with Error Return

```go
processWithError := func(id string) error {
    // Simulate work
    if id == "" {
        return fmt.Errorf("invalid id")
    }
    fmt.Printf("Processed: %s\n", id)
    return nil
}

sched.AddJob(processWithError,
    trigger.NewIntervalTrigger(10*time.Second),
    job.WithArgs("item-123"),
)

// Listen for errors
sched.AddListener(func(e event.Event) {
    if evt, ok := e.(*event.JobExecutionEvent); ok && evt.Exception != nil {
        log.Printf("Job error: %v", evt.Exception)
    }
}, event.JobError)
```

### Panic Recovery

The executor automatically recovers from panics:

```go
riskyJob := func() {
    panic("something went wrong!")
}

sched.AddJob(riskyJob, trigger.NewIntervalTrigger(10*time.Second))

// Panic is caught and reported as JobError event
sched.AddListener(func(e event.Event) {
    if evt, ok := e.(*event.JobExecutionEvent); ok && evt.Exception != nil {
        fmt.Printf("Job panicked: %v\nStack: %s\n",
            evt.Exception, evt.Traceback)
    }
}, event.JobError)
```
