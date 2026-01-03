# Job Stores

Job stores persist job state. This document covers all available job store backends and their configuration.

## Overview

Job stores are responsible for:
- Persisting job definitions and state
- Tracking next run times
- Providing due jobs for execution
- Enabling job recovery after restarts (persistent stores)

## In-Memory Store

The default store. Jobs are lost when the application stops.

```go
import "github.com/georgepadayatti/goscheduler/jobstore"

store := jobstore.NewMemoryJobStore()
sched.AddJobStore(store, "default")
```

**Characteristics:**
- No external dependencies
- Fast operations (binary search for efficiency)
- Jobs sorted by next run time
- No persistence across restarts

## Redis Store

Persists jobs in Redis using a hash for jobs and a sorted set for run times.

```go
import "github.com/georgepadayatti/goscheduler/jobstore"

store, err := jobstore.NewRedisJobStore(jobstore.RedisJobStoreConfig{
    Addr:        "localhost:6379",
    Password:    "",              // Optional
    DB:          0,               // Redis database number
    JobsKey:     "scheduler.jobs",
    RunTimesKey: "scheduler.run_times",
})
if err != nil {
    log.Fatal(err)
}
sched.AddJobStore(store, "redis")
```

**Configuration:**

| Field | Description | Default |
|-------|-------------|---------|
| `Addr` | Redis server address | Required |
| `Password` | Redis password | "" |
| `DB` | Redis database number | 0 |
| `JobsKey` | Key for jobs hash | Required |
| `RunTimesKey` | Key for run times sorted set | Required |

**Requirements:**
- `FuncRef` is auto-derived from the function name if omitted
- Function must be registered before restoration

```go
job.RegisterFuncByName(myFunc)
sched.AddJob(myFunc, trigger)
```

## PostgreSQL Store

Persists jobs in a PostgreSQL database.

```go
import "github.com/georgepadayatti/goscheduler/jobstore"

store, err := jobstore.NewPostgresJobStore(jobstore.PostgresConfig{
    DSN:       "postgres://user:password@localhost/dbname?sslmode=disable",
    TableName: "scheduler_jobs",
})
if err != nil {
    log.Fatal(err)
}
sched.AddJobStore(store, "postgres")
```

**Configuration:**

| Field | Description | Default |
|-------|-------------|---------|
| `DSN` | PostgreSQL connection string | Required |
| `TableName` | Table name for jobs | Required |

**Table Schema:**

The store automatically creates the table with this schema:

```sql
CREATE TABLE IF NOT EXISTS scheduler_jobs (
    id TEXT PRIMARY KEY,
    job_state BYTEA NOT NULL,
    next_run_time TIMESTAMP WITH TIME ZONE
);
CREATE INDEX IF NOT EXISTS idx_next_run_time ON scheduler_jobs(next_run_time);
```

## MySQL Store

Persists jobs in a MySQL database.

```go
import "github.com/georgepadayatti/goscheduler/jobstore"

store, err := jobstore.NewMySQLJobStore(jobstore.MySQLConfig{
    DSN:       "user:password@tcp(localhost:3306)/dbname",
    TableName: "scheduler_jobs",
})
if err != nil {
    log.Fatal(err)
}
sched.AddJobStore(store, "mysql")
```

**Configuration:**

| Field | Description | Default |
|-------|-------------|---------|
| `DSN` | MySQL connection string | Required |
| `TableName` | Table name for jobs | Required |

## MongoDB Store

Persists jobs in MongoDB.

```go
import "github.com/georgepadayatti/goscheduler/jobstore"

store, err := jobstore.NewMongoDBJobStore(jobstore.MongoDBConfig{
    URI:            "mongodb://localhost:27017",
    Database:       "scheduler",
    CollectionName: "jobs",
})
if err != nil {
    log.Fatal(err)
}
sched.AddJobStore(store, "mongodb")
```

**Configuration:**

| Field | Description | Default |
|-------|-------------|---------|
| `URI` | MongoDB connection URI | Required |
| `Database` | Database name | Required |
| `CollectionName` | Collection name for jobs | Required |

## Zookeeper Store

Persists jobs in Apache Zookeeper. Suitable for distributed scheduling.

```go
import "github.com/georgepadayatti/goscheduler/jobstore"

store, err := jobstore.NewZookeeperJobStore(jobstore.ZookeeperConfig{
    Servers:  []string{"localhost:2181"},
    BasePath: "/scheduler/jobs",
})
if err != nil {
    log.Fatal(err)
}
sched.AddJobStore(store, "zookeeper")
```

**Configuration:**

| Field | Description | Default |
|-------|-------------|---------|
| `Servers` | Zookeeper server addresses | Required |
| `BasePath` | Base path for job znodes | Required |
| `SessionTimeout` | Session timeout | 10s |

## etcd Store

Persists jobs in etcd. Suitable for distributed scheduling with leader election.

```go
import "github.com/georgepadayatti/goscheduler/jobstore"

store, err := jobstore.NewEtcdJobStore(jobstore.EtcdConfig{
    Endpoints: []string{"localhost:2379"},
    KeyPrefix: "/scheduler/jobs/",
})
if err != nil {
    log.Fatal(err)
}
sched.AddJobStore(store, "etcd")
```

**Configuration:**

| Field | Description | Default |
|-------|-------------|---------|
| `Endpoints` | etcd server endpoints | Required |
| `KeyPrefix` | Key prefix for jobs | Required |
| `DialTimeout` | Connection timeout | 5s |
| `Username` | etcd username | "" |
| `Password` | etcd password | "" |

## Multiple Job Stores

You can use multiple job stores simultaneously:

```go
// In-memory for transient jobs
memStore := jobstore.NewMemoryJobStore()
sched.AddJobStore(memStore, "memory")

// Redis for persistent jobs
redisStore, _ := jobstore.NewRedisJobStore(redisConfig)
sched.AddJobStore(redisStore, "persistent")

// Add jobs to specific stores
sched.AddJob(transientJob, trigger,
    job.WithJobStore("memory"),
)

job.RegisterFuncByName(persistentJob)
sched.AddJob(persistentJob, trigger,
    job.WithJobStore("persistent"),
)
```

## Job Store Interface

All job stores implement this interface:

```go
type JobStore interface {
    // Lifecycle
    Start(scheduler interface{}, alias string) error
    Shutdown() error

    // Job operations
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

## Error Types

```go
// Job not found
type JobLookupError struct {
    JobID string
}

// Duplicate job ID
type ConflictingIDError struct {
    JobID string
}

// Job cannot be serialized (missing FuncRef)
type TransientJobError struct {
    JobID string
}
```

## Custom Job Store

Create a custom job store by implementing the `JobStore` interface:

```go
type MyJobStore struct {
    scheduler interface{}
    alias     string
    jobs      map[string]*job.Job
    mu        sync.RWMutex
}

func (s *MyJobStore) Start(scheduler interface{}, alias string) error {
    s.scheduler = scheduler
    s.alias = alias
    s.jobs = make(map[string]*job.Job)
    return nil
}

func (s *MyJobStore) Shutdown() error {
    return nil
}

func (s *MyJobStore) AddJob(j *job.Job) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if _, exists := s.jobs[j.ID]; exists {
        return &jobstore.ConflictingIDError{JobID: j.ID}
    }
    s.jobs[j.ID] = j
    return nil
}

// ... implement remaining methods
```

## Best Practices

1. **Use persistent stores for critical jobs**: Redis, PostgreSQL, or MongoDB for jobs that must survive restarts

2. **Register functions for persistent stores**: `FuncRef` is derived automatically, but functions must be registered on startup

3. **Use appropriate store for scale**:
   - Small scale: In-memory or PostgreSQL
   - Large scale: Redis
   - Distributed: etcd or Zookeeper

4. **Handle connection failures**: Configure retry intervals in scheduler config

5. **Use separate stores for different job types**: Transient vs persistent, high-priority vs low-priority
