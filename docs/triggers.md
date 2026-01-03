# Triggers

Triggers determine when jobs should execute. This document covers all available trigger types and their configuration options.

## Trigger Interface

All triggers implement the `Trigger` interface:

```go
type Trigger interface {
    // GetNextFireTime calculates the next execution time
    // Returns nil when the trigger is exhausted
    GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time

    // String returns a human-readable description
    String() string
}
```

## Interval Trigger

Executes jobs at fixed time intervals.

### Basic Usage

```go
import "github.com/georgepadayatti/goscheduler/trigger"

// Every 5 seconds
trig := trigger.NewIntervalTrigger(5 * time.Second)

// Every 2 hours
trig := trigger.NewIntervalTrigger(2 * time.Hour)
```

### Options

| Option | Description |
|--------|-------------|
| `WithIntervalStartDate(time)` | Don't fire before this time |
| `WithIntervalEndDate(time)` | Don't fire after this time |
| `WithIntervalJitter(duration)` | Add random delay up to this value |
| `WithIntervalLocation(loc)` | Timezone for calculations |

### Examples

```go
// With jitter to prevent thundering herd
trig := trigger.NewIntervalTrigger(10*time.Second,
    trigger.WithIntervalJitter(2*time.Second))

// Limited time window
startTime := time.Now().Add(1 * time.Hour)
endTime := time.Now().Add(24 * time.Hour)
trig := trigger.NewIntervalTrigger(5*time.Second,
    trigger.WithIntervalStartDate(startTime),
    trigger.WithIntervalEndDate(endTime))

// Specific timezone
loc, _ := time.LoadLocation("America/New_York")
trig := trigger.NewIntervalTrigger(1*time.Hour,
    trigger.WithIntervalLocation(loc))
```

## Cron Trigger

Standard cron-style scheduling with support for seconds.

### Basic Usage

```go
import "github.com/georgepadayatti/goscheduler/trigger/cron"

// From crontab string (5 fields: minute hour day month day_of_week)
trig, err := cron.FromCrontab("0 9 * * MON", time.Local)  // 9 AM every Monday

// Using builder pattern
trig, err := cron.NewCronTrigger(
    cron.WithMinute("0"),
    cron.WithHour("9"),
    cron.WithCronLocation(time.Local),
)
```

### Cron Fields

| Field | Allowed Values | Special Characters |
|-------|---------------|-------------------|
| Second | 0-59 | * , - / |
| Minute | 0-59 | * , - / |
| Hour | 0-23 | * , - / |
| Day | 1-31 | * , - / |
| Month | 1-12 | * , - / |
| Day of Week | 0-6 (0=Sunday) | * , - / |
| Year | 1970-2099 | * , - / |

### Options

| Option | Description |
|--------|-------------|
| `WithSecond(expr)` | Second field (default: "0") |
| `WithMinute(expr)` | Minute field (default: "*") |
| `WithHour(expr)` | Hour field (default: "*") |
| `WithDay(expr)` | Day of month field (default: "*") |
| `WithMonth(expr)` | Month field (default: "*") |
| `WithDayOfWeek(expr)` | Day of week field (default: "*") |
| `WithYear(expr)` | Year field (default: "*") |
| `WithCronStartDate(time)` | Don't fire before this time |
| `WithCronEndDate(time)` | Don't fire after this time |
| `WithCronLocation(loc)` | Timezone |

### Examples

```go
// Every minute
trig, _ := cron.NewCronTrigger(
    cron.WithSecond("0"),
    cron.WithCronLocation(time.Local),
)

// At 9:30 AM on weekdays
trig, _ := cron.NewCronTrigger(
    cron.WithSecond("0"),
    cron.WithMinute("30"),
    cron.WithHour("9"),
    cron.WithDayOfWeek("1-5"),  // Monday through Friday
    cron.WithCronLocation(time.Local),
)

// First day of every month at midnight
trig, _ := cron.NewCronTrigger(
    cron.WithSecond("0"),
    cron.WithMinute("0"),
    cron.WithHour("0"),
    cron.WithDay("1"),
    cron.WithCronLocation(time.Local),
)

// Every 15 minutes
trig, _ := cron.NewCronTrigger(
    cron.WithSecond("0"),
    cron.WithMinute("*/15"),
    cron.WithCronLocation(time.Local),
)
```

### Crontab Format

The `FromCrontab` function accepts standard 5-field crontab expressions:

```
┌───────────── minute (0-59)
│ ┌───────────── hour (0-23)
│ │ ┌───────────── day of month (1-31)
│ │ │ ┌───────────── month (1-12)
│ │ │ │ ┌───────────── day of week (0-6, 0=Sunday)
│ │ │ │ │
* * * * *
```

Examples:
- `0 9 * * *` - Every day at 9:00 AM
- `*/5 * * * *` - Every 5 minutes
- `0 0 1 * *` - First day of every month at midnight
- `0 9 * * 1-5` - Weekdays at 9:00 AM
- `0 9,17 * * *` - At 9:00 AM and 5:00 PM

## Date Trigger

Fires once at a specific date and time.

### Usage

```go
import "github.com/georgepadayatti/goscheduler/trigger"

// Fire at a specific time
alarmTime := time.Date(2026, 1, 15, 14, 30, 0, 0, time.Local)
trig := trigger.NewDateTrigger(alarmTime)

// With timezone
loc, _ := time.LoadLocation("Europe/London")
alarmTime := time.Date(2026, 6, 1, 9, 0, 0, 0, loc)
trig := trigger.NewDateTrigger(alarmTime, trigger.WithDateLocation(loc))
```

The trigger returns `nil` after firing once, causing the job to be removed.

## Calendar Interval Trigger

Fires at calendar-based intervals (years, months, weeks, days) at a fixed time of day.

### Options

| Option | Description |
|--------|-------------|
| `WithCalendarYears(n)` | Interval in years |
| `WithCalendarMonths(n)` | Interval in months |
| `WithCalendarWeeks(n)` | Interval in weeks |
| `WithCalendarDays(n)` | Interval in days |
| `WithCalendarHour(h)` | Hour of day (0-23) |
| `WithCalendarMinute(m)` | Minute (0-59) |
| `WithCalendarSecond(s)` | Second (0-59) |
| `WithCalendarStartDate(time)` | Don't fire before this time |
| `WithCalendarEndDate(time)` | Don't fire after this time |
| `WithCalendarLocation(loc)` | Timezone |

### Examples

```go
import "github.com/georgepadayatti/goscheduler/trigger"

// Every month on the 1st at 10:00 AM
trig := trigger.NewCalendarIntervalTrigger(
    trigger.WithCalendarMonths(1),
    trigger.WithCalendarHour(10),
    trigger.WithCalendarMinute(0),
)

// Every 2 weeks at 9:30 AM
trig := trigger.NewCalendarIntervalTrigger(
    trigger.WithCalendarWeeks(2),
    trigger.WithCalendarHour(9),
    trigger.WithCalendarMinute(30),
)

// Quarterly at midnight
trig := trigger.NewCalendarIntervalTrigger(
    trigger.WithCalendarMonths(3),
    trigger.WithCalendarHour(0),
    trigger.WithCalendarMinute(0),
)

// Annually
trig := trigger.NewCalendarIntervalTrigger(
    trigger.WithCalendarYears(1),
    trigger.WithCalendarHour(0),
    trigger.WithCalendarMinute(0),
)
```

## Combining Triggers

Combine multiple triggers using AND/OR logic.

### AND Trigger

Fires when all triggers agree on the next fire time. Finished when any trigger finishes.

```go
import "github.com/georgepadayatti/goscheduler/trigger"

// Must satisfy both triggers
trig := trigger.NewAndTrigger(trigger1, trigger2)
```

### OR Trigger

Fires at the earliest time from any trigger. Finished when all triggers finish.

```go
import "github.com/georgepadayatti/goscheduler/trigger"

// Fire when either trigger fires
trig := trigger.NewOrTrigger(trigger1, trigger2)
```

### Example: Business Hours Only

```go
// Run every 5 minutes, but only during business hours (9 AM - 5 PM)
intervalTrig := trigger.NewIntervalTrigger(5 * time.Minute)

businessHoursTrig, _ := cron.NewCronTrigger(
    cron.WithHour("9-17"),
    cron.WithDayOfWeek("1-5"),  // Weekdays
    cron.WithCronLocation(time.Local),
)

combinedTrig := trigger.NewAndTrigger(intervalTrig, businessHoursTrig)
```

## Jitter

Jitter adds randomness to fire times to prevent multiple jobs from executing simultaneously (thundering herd problem).

Triggers that support jitter implement the `Jitterable` interface:

```go
type Jitterable interface {
    SetJitter(jitter time.Duration)
}
```

Interval triggers accept jitter as an option:

```go
// Fire every 10 seconds, plus random delay of 0-2 seconds
trig := trigger.NewIntervalTrigger(10*time.Second,
    trigger.WithIntervalJitter(2*time.Second))
```

## Custom Triggers

Create custom triggers by implementing the `Trigger` interface:

```go
type MyTrigger struct {
    fireCount int
    maxFires  int
}

func (t *MyTrigger) GetNextFireTime(prev *time.Time, now time.Time) *time.Time {
    if t.fireCount >= t.maxFires {
        return nil  // Trigger exhausted
    }
    t.fireCount++
    next := now.Add(10 * time.Second)
    return &next
}

func (t *MyTrigger) String() string {
    return fmt.Sprintf("MyTrigger(max=%d)", t.maxFires)
}
```
