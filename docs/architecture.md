# Architecture

This document describes the architecture and design of GoScheduler.

## Overview

GoScheduler is built around a modular architecture with pluggable components. The main components are:

```
┌─────────────────────────────────────────────────────────────┐
│                        Scheduler                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Jobs      │  │  Triggers   │  │   Event System      │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │             │
│         ▼                ▼                     ▼             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  Job Store  │  │  Executor   │  │     Listeners       │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Core Components

### Scheduler

The scheduler is the central coordinator that manages jobs, triggers execution, and dispatches events.

**Two implementations:**

| Type | Behavior | Use Case |
|------|----------|----------|
| `BackgroundScheduler` | `Start()` returns immediately | Web servers, long-running services |
| `BlockingScheduler` | `Start()` blocks until shutdown | CLI tools, standalone applications |

Both share a common `BaseScheduler` implementation that handles:

- Job lifecycle management (add, modify, pause, resume, remove)
- Job processing loop with timer-based wakeup
- Event dispatching to registered listeners
- Graceful shutdown with optional wait

**States:**
- `Stopped` - Scheduler not running
- `Running` - Actively processing jobs
- `Paused` - Running but not executing jobs

### Job

A job represents a unit of work to be executed. Each job contains:

| Field | Description |
|-------|-------------|
| `ID` | Unique identifier (auto-generated UUID if not set) |
| `Name` | Human-readable name |
| `Func` | The Go function to execute |
| `FuncRef` | String reference for serialization (auto-derived from function name for persistent stores) |
| `Args` | Arguments passed to the function |
| `Trigger` | Determines when the job runs |
| `Executor` | Which executor runs this job |
| `JobStore` | Which store persists this job |
| `NextRunTime` | When the job will next execute |
| `Coalesce` | Whether to combine overdue runs |
| `MisfireGraceTime` | Grace period for late execution |
| `MaxInstances` | Maximum concurrent executions |

### Trigger

Triggers determine when jobs should execute. All triggers implement:

```go
type Trigger interface {
    GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time
    String() string
}
```

Available triggers:
- **IntervalTrigger** - Fixed intervals (every N seconds/minutes/hours)
- **CronTrigger** - Cron expressions
- **DateTrigger** - One-time execution
- **CalendarIntervalTrigger** - Calendar-based intervals
- **AndTrigger** / **OrTrigger** - Combining multiple triggers

### Executor

Executors run job functions. The default `GoroutinePoolExecutor`:

- Maintains a worker pool (default 10 workers)
- Uses reflection to invoke functions
- Handles panic recovery
- Tracks concurrent instances per job
- Enforces `MaxInstances` limits
- Dispatches execution events

```go
type Executor interface {
    Start(scheduler interface{}, alias string) error
    Shutdown(wait bool) error
    SubmitJob(j *job.Job, runTimes []time.Time) error
}
```

### Job Store

Job stores persist job state. All stores implement:

```go
type JobStore interface {
    Start(scheduler interface{}, alias string) error
    Shutdown() error
    LookupJob(jobID string) (*job.Job, error)
    GetDueJobs(now time.Time) ([]*job.Job, error)
    GetNextRunTime() *time.Time
    GetAllJobs() ([]*job.Job, error)
    AddJob(j *job.Job) error
    UpdateJob(j *job.Job) error
    RemoveJob(jobID string) error
    RemoveAllJobs() error
}
```

Available stores:
- **MemoryJobStore** - In-memory (default)
- **RedisJobStore** - Redis backend
- **PostgresJobStore** - PostgreSQL backend
- **MySQLJobStore** - MySQL backend
- **MongoDBJobStore** - MongoDB backend
- **ZookeeperJobStore** - Zookeeper backend
- **EtcdJobStore** - etcd backend

### Event System

The event system provides notifications for scheduler and job lifecycle events.

**Event types:**
- Scheduler: `Started`, `Shutdown`, `Paused`, `Resumed`
- Component: `ExecutorAdded`, `ExecutorRemoved`, `JobStoreAdded`, `JobStoreRemoved`
- Job lifecycle: `JobAdded`, `JobRemoved`, `JobModified`
- Job execution: `JobSubmitted`, `JobExecuted`, `JobError`, `JobMissed`, `JobMaxInstances`

Events use a bitmask system for efficient filtering:

```go
// Listen to specific events
sched.AddListener(callback, event.JobExecuted | event.JobError)

// Listen to all events
sched.AddListener(callback, event.All)
```

## Job Lifecycle

```
┌──────────┐    AddJob()    ┌──────────┐
│  Create  │ ─────────────► │  Stored  │
└──────────┘                └────┬─────┘
                                 │
                                 ▼
                           ┌──────────┐
                           │  Queued  │◄────────────┐
                           └────┬─────┘             │
                                │                   │
                    NextRunTime <= now              │
                                │                   │
                                ▼                   │
                           ┌──────────┐             │
                           │ Submitted│             │
                           └────┬─────┘             │
                                │                   │
                                ▼                   │
                           ┌──────────┐     Update NextRunTime
                           │ Executed │─────────────┘
                           └────┬─────┘
                                │
                    Trigger returns nil
                                │
                                ▼
                           ┌──────────┐
                           │ Removed  │
                           └──────────┘
```

## Processing Loop

The scheduler runs a main loop that:

1. Calls `ProcessJobs()` periodically
2. Gets due jobs from store (`NextRunTime <= now`)
3. Handles coalescing for multiple overdue runs
4. Submits jobs to the executor
5. Updates `NextRunTime` from trigger
6. Sleeps until next job is due (timer-based, no busy polling)

## Thread Safety

All components use appropriate synchronization:

- `sync.RWMutex` for concurrent read/write access
- Goroutine-safe schedulers, executors, and stores
- Event dispatching is asynchronous by default

## Serialization

For persistent job stores, jobs must be serializable:

- Triggers are serialized using `encoding/gob`
- Functions are referenced by string (`FuncRef`), derived from function names by default, and must be registered
- Arguments must be gob-serializable

```go
// Register function reference for persistent stores
job.RegisterFuncByName(myFunc)
sched.AddJob(myFunc, trigger)
```

## Extensibility

The architecture supports custom implementations:

- **Custom Triggers**: Implement the `Trigger` interface
- **Custom Executors**: Implement the `Executor` interface
- **Custom Job Stores**: Implement the `JobStore` interface
- **Custom Event Handlers**: Register listeners for any event combination
