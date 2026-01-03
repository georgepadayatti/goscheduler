# GoScheduler

A feature-rich, extensible job scheduling library for Go with support for multiple backends and flexible trigger types.

> **Disclaimer:** This is a fun experiment; use at your peril. It is not intended for production use.

## Features

- **Multiple Trigger Types**: Interval, Cron, Date, Calendar, and combining triggers (AND/OR)
- **Pluggable Job Stores**: In-memory, Redis, PostgreSQL, MySQL, MongoDB, etcd, Zookeeper
- **Concurrent Execution**: Goroutine pool executor with configurable worker count
- **Event System**: Subscribe to scheduler and job lifecycle events
- **Misfire Handling**: Configurable grace periods and coalescing for overdue jobs
- **Thread-Safe**: All components are safe for concurrent use
- **Two Scheduler Modes**: Background (non-blocking) and Blocking schedulers

## Installation

```bash
go get github.com/georgepadayatti/goscheduler
```

## Quick Start

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
    // Create a background scheduler
    sched := scheduler.NewBackgroundScheduler(scheduler.DefaultConfig())

    // Define a job function
    printMessage := func(msg string) {
        fmt.Printf("[%s] %s\n", time.Now().Format(time.RFC3339), msg)
    }

    // Add a job with an interval trigger (runs every 5 seconds)
    sched.AddJob(
        printMessage,
        trigger.NewIntervalTrigger(5*time.Second),
        job.WithID("my-job"),
        job.WithName("Print Message"),
        job.WithArgs("Hello, GoScheduler!"),
    )

    // Start the scheduler
    ctx := context.Background()
    sched.Start(ctx)

    // Run for 30 seconds
    time.Sleep(30 * time.Second)

    // Shutdown gracefully
    sched.Shutdown(true)
}
```

## Trigger Types

### Interval Trigger

Executes jobs at fixed intervals:

```go
// Every 10 seconds
trig := trigger.NewIntervalTrigger(10 * time.Second)

// With jitter to prevent clustering
trig := trigger.NewIntervalTrigger(10*time.Second,
    trigger.WithIntervalJitter(2*time.Second))

// With start and end dates
trig := trigger.NewIntervalTrigger(5*time.Second,
    trigger.WithIntervalStartDate(startTime),
    trigger.WithIntervalEndDate(endTime))
```

### Cron Trigger

Standard cron-style scheduling:

```go
import "github.com/georgepadayatti/goscheduler/trigger/cron"

// From crontab expression (5 fields: minute, hour, day, month, day_of_week)
trig, _ := cron.FromCrontab("0 9 * * MON", time.Local)  // 9 AM every Monday

// Using builder pattern
trig, _ := cron.NewCronTrigger(
    cron.WithMinute("0"),
    cron.WithHour("9"),
    cron.WithDayOfWeek("1"),  // Monday
    cron.WithCronLocation(time.Local),
)
```

### Date Trigger

One-time execution at a specific datetime:

```go
alarmTime := time.Date(2026, 1, 15, 14, 30, 0, 0, time.Local)
trig := trigger.NewDateTrigger(alarmTime)
```

### Calendar Interval Trigger

Calendar-based intervals at fixed times:

```go
// Every 2 months at 10:30:00
trig := trigger.NewCalendarIntervalTrigger(
    trigger.WithCalendarMonths(2),
    trigger.WithCalendarHour(10),
    trigger.WithCalendarMinute(30),
)
```

### Combining Triggers

Combine multiple triggers with AND/OR logic:

```go
// AND: waits for agreement from all triggers
trig := trigger.NewAndTrigger(trigger1, trigger2)

// OR: fires at earliest time from any trigger
trig := trigger.NewOrTrigger(trigger1, trigger2)
```

## Job Configuration

```go
sched.AddJob(
    myFunction,
    myTrigger,
    job.WithID("unique-id"),           // Custom job ID
    job.WithName("My Job"),            // Human-readable name
    job.WithArgs("arg1", 42),          // Function arguments
    job.WithMaxInstances(3),           // Max concurrent executions
    job.WithCoalesce(true),            // Combine overdue runs
    job.WithMisfireGraceTime(5*time.Second), // Grace period
    job.WithExecutor("custom"),        // Use custom executor
    job.WithJobStore("redis"),         // Use specific job store
)
```

## Job Stores

### In-Memory (Default)

```go
store := jobstore.NewMemoryJobStore()
sched.AddJobStore(store, "default")
```

### Redis

```go
store, _ := jobstore.NewRedisJobStore(jobstore.RedisJobStoreConfig{
    Addr:        "localhost:6379",
    Password:    "",
    DB:          0,
    JobsKey:     "scheduler.jobs",
    RunTimesKey: "scheduler.run_times",
})
sched.AddJobStore(store, "redis")
```

### PostgreSQL

```go
store, _ := jobstore.NewPostgresJobStore(jobstore.PostgresConfig{
    DSN:       "postgres://user:pass@localhost/dbname?sslmode=disable",
    TableName: "scheduler_jobs",
})
sched.AddJobStore(store, "postgres")
```

## Event Listening

```go
import "github.com/georgepadayatti/goscheduler/event"

sched.AddListener(func(e event.Event) {
    switch evt := e.(type) {
    case *event.JobExecutionEvent:
        if evt.Exception != nil {
            fmt.Printf("Job %s failed: %v\n", evt.JobID, evt.Exception)
        } else {
            fmt.Printf("Job %s completed\n", evt.JobID)
        }
    case *event.SchedulerEvent:
        fmt.Printf("Scheduler event: %s\n", evt.Code)
    }
}, event.JobExecuted|event.JobError|event.SchedulerStarted)
```

## Job Management

```go
// Get a job
job, _ := sched.GetJob("my-job")

// Get all jobs
jobs, _ := sched.GetJobs()

// Pause a job
sched.PauseJob("my-job")

// Resume a job
sched.ResumeJob("my-job")

// Reschedule with a new trigger
sched.RescheduleJob("my-job", newTrigger)

// Remove a job
sched.RemoveJob("my-job")
```

## Scheduler Control

```go
// Start the scheduler
sched.Start(ctx)

// Pause all jobs
sched.Pause()

// Resume all jobs
sched.Resume()

// Check scheduler state
state := sched.State()  // Stopped, Running, or Paused

// Shutdown (wait=true waits for running jobs to complete)
sched.Shutdown(true)
```

## Documentation

See the [docs](./docs) folder for detailed documentation:

- [Architecture](./docs/architecture.md) - System design and components
- [Triggers](./docs/triggers.md) - Detailed trigger documentation
- [Job Stores](./docs/jobstores.md) - Storage backend options
- [Configuration](./docs/configuration.md) - Configuration options
- [Examples](./docs/examples.md) - More usage examples

## License

This project is licensed under the Apache License 2.0 - see the LICENSE file for details.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
