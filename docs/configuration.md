# Configuration

This document covers all configuration options for GoScheduler.

## Scheduler Configuration

Create a scheduler with custom configuration:

```go
import "github.com/georgepadayatti/goscheduler/scheduler"

config := scheduler.Config{
    Timezone:                time.UTC,
    JobStoreRetryInterval:   10 * time.Second,
    DefaultMisfireGraceTime: 0,
    DefaultCoalesce:         true,
    DefaultMaxInstances:     1,
}

sched := scheduler.NewBackgroundScheduler(config)
// or
sched := scheduler.NewBlockingScheduler(config)
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `Timezone` | `*time.Location` | `time.Local` | Timezone for time calculations |
| `JobStoreRetryInterval` | `time.Duration` | `10s` | Retry interval when job store operations fail |
| `DefaultMisfireGraceTime` | `time.Duration` | `0` | Default grace period for late job execution (0 disables) |
| `DefaultCoalesce` | `bool` | `true` | Default coalesce setting for jobs |
| `DefaultMaxInstances` | `int` | `1` | Default max concurrent instances per job |

### Default Configuration

Use the default configuration:

```go
config := scheduler.DefaultConfig()
```

## Job Options

Configure individual jobs using functional options:

```go
import "github.com/georgepadayatti/goscheduler/job"

sched.AddJob(
    myFunction,
    myTrigger,
    job.WithID("unique-id"),
    job.WithName("My Job"),
    job.WithArgs("arg1", 42, true),
    job.WithExecutor("pool"),
    job.WithJobStore("redis"),
    job.WithCoalesce(true),
    job.WithMisfireGraceTime(5*time.Second),
    job.WithMaxInstances(3),
)
```

### Available Job Options

| Option | Description |
|--------|-------------|
| `WithID(id string)` | Set a custom job ID (default: auto-generated UUID) |
| `WithName(name string)` | Set a human-readable name (default: function name) |
| `WithArgs(args ...interface{})` | Arguments passed to the job function |
| `WithFuncRef(ref string)` | Function reference for serialization (auto-derived from function name if omitted; required for persistent stores) |
| `WithExecutor(alias string)` | Executor to use (default: "default") |
| `WithJobStore(alias string)` | Job store to use (default: "default") |
| `WithCoalesce(coalesce bool)` | Combine multiple overdue runs into one |
| `WithMisfireGraceTime(d time.Duration)` | Grace period for late execution |
| `WithNoMisfireGraceTime()` | Disable misfire grace time |
| `WithMaxInstances(max int)` | Maximum concurrent executions |
| `WithNextRunTime(t time.Time)` | Set initial next run time (for restoration) |

If you rely on persistent stores and omit `WithFuncRef`, register the function on startup so jobs can be restored:

```go
job.RegisterFuncByName(myFunction)
```

## Executor Configuration

### Goroutine Pool Executor

The default executor with configurable worker pool:

```go
import "github.com/georgepadayatti/goscheduler/executor"

exec := executor.NewGoroutinePoolExecutor(
    executor.WithMaxWorkers(20),  // Default: 10
)
sched.AddExecutor(exec, "pool")
```

### Executor Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithMaxWorkers(n int)` | 10 | Maximum concurrent worker goroutines |

## Job Store Configuration

See [Job Stores](./jobstores.md) for detailed configuration of each store type.

### Common Patterns

**Multiple stores:**

```go
// Memory store for transient jobs
memStore := jobstore.NewMemoryJobStore()
sched.AddJobStore(memStore, "memory")

// Redis store for persistent jobs
redisStore, _ := jobstore.NewRedisJobStore(redisConfig)
sched.AddJobStore(redisStore, "persistent")
```

**Default store:**

```go
// The "default" alias is used when no store is specified
sched.AddJobStore(store, "default")
```

## Misfire Handling

### What is a Misfire?

A misfire occurs when a job's scheduled run time passes before it can be executed. This can happen due to:
- Application restart
- Heavy system load
- Scheduler being paused

### Grace Time

The misfire grace time determines how late a job can run before being considered missed:

```go
// Job-level setting
job.WithMisfireGraceTime(30 * time.Second)

// Disable grace time (always execute)
job.WithNoMisfireGraceTime()
```

If a job runs later than `NextRunTime + MisfireGraceTime`, it's marked as missed and the `JobMissed` event is dispatched.

### Coalescing

When multiple runs are overdue, coalescing combines them into a single execution:

```go
// Enable coalescing (default)
job.WithCoalesce(true)

// Disable coalescing (run each overdue execution)
job.WithCoalesce(false)
```

## Concurrency Control

### Max Instances

Limit concurrent executions of the same job:

```go
// Allow up to 3 concurrent executions
job.WithMaxInstances(3)
```

When the limit is reached, new executions are skipped and the `JobMaxInstances` event is dispatched.

### Executor Workers

Control the total worker pool size:

```go
exec := executor.NewGoroutinePoolExecutor(
    executor.WithMaxWorkers(50),
)
```

## Event Configuration

### Listener Registration

Register listeners for specific events:

```go
import "github.com/georgepadayatti/goscheduler/event"

// Listen to job execution events
sched.AddListener(callback, event.JobExecuted|event.JobError)

// Listen to all events
sched.AddListener(callback, event.All)

// Listen to scheduler lifecycle
sched.AddListener(callback, event.SchedulerStarted|event.SchedulerShutdown)
```

### Event Codes

| Event | Description |
|-------|-------------|
| `SchedulerStarted` | Scheduler started |
| `SchedulerShutdown` | Scheduler shut down |
| `SchedulerPaused` | Scheduler paused |
| `SchedulerResumed` | Scheduler resumed |
| `ExecutorAdded` | Executor added |
| `ExecutorRemoved` | Executor removed |
| `JobStoreAdded` | Job store added |
| `JobStoreRemoved` | Job store removed |
| `JobAdded` | Job added |
| `JobRemoved` | Job removed |
| `JobModified` | Job modified |
| `JobSubmitted` | Job submitted for execution |
| `JobExecuted` | Job executed successfully |
| `JobError` | Job execution failed |
| `JobMissed` | Job missed due to misfire |
| `JobMaxInstances` | Job skipped due to max instances |
| `All` | All events |

## Environment-Specific Configuration

### Development

```go
config := scheduler.Config{
    Timezone:                time.Local,
    JobStoreRetryInterval:   5 * time.Second,
    DefaultMisfireGraceTime: 10 * time.Second,  // More lenient
    DefaultCoalesce:         true,
    DefaultMaxInstances:     1,
}
```

### Production

```go
config := scheduler.Config{
    Timezone:                time.UTC,           // Consistent timezone
    JobStoreRetryInterval:   30 * time.Second,   // More retries
    DefaultMisfireGraceTime: 1 * time.Second,    // Strict timing
    DefaultCoalesce:         true,
    DefaultMaxInstances:     1,
}
```

### High-Throughput

```go
config := scheduler.DefaultConfig()

// Use larger worker pool
exec := executor.NewGoroutinePoolExecutor(
    executor.WithMaxWorkers(100),
)
sched.AddExecutor(exec, "default")

// Use Redis for persistence
redisStore, _ := jobstore.NewRedisJobStore(redisConfig)
sched.AddJobStore(redisStore, "default")
```
