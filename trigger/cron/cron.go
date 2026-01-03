package cron

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"strings"
	"time"

	"github.com/georgejpadayatti/goscheduler/internal/timeutil"
)

// FieldNames defines the order of cron fields.
var FieldNames = []string{
	"year", "month", "day", "week", "day_of_week",
	"hour", "minute", "second",
}

// CronTrigger fires when current time matches all specified time constraints.
type CronTrigger struct {
	Fields    []Field
	StartDate *time.Time
	EndDate   *time.Time
	Location  *time.Location
	Jitter    time.Duration
}

// CronOption configures a CronTrigger.
type CronOption func(*cronConfig)

type cronConfig struct {
	year       string
	month      string
	day        string
	week       string
	dayOfWeek  string
	hour       string
	minute     string
	second     string
	startDate  *time.Time
	endDate    *time.Time
	location   *time.Location
	jitter     time.Duration
}

// WithYear sets the year field.
func WithYear(expr string) CronOption {
	return func(c *cronConfig) { c.year = expr }
}

// WithMonth sets the month field.
func WithMonth(expr string) CronOption {
	return func(c *cronConfig) { c.month = expr }
}

// WithDay sets the day of month field.
func WithDay(expr string) CronOption {
	return func(c *cronConfig) { c.day = expr }
}

// WithWeek sets the week field.
func WithWeek(expr string) CronOption {
	return func(c *cronConfig) { c.week = expr }
}

// WithDayOfWeek sets the day of week field.
func WithDayOfWeek(expr string) CronOption {
	return func(c *cronConfig) { c.dayOfWeek = expr }
}

// WithHour sets the hour field.
func WithHour(expr string) CronOption {
	return func(c *cronConfig) { c.hour = expr }
}

// WithMinute sets the minute field.
func WithMinute(expr string) CronOption {
	return func(c *cronConfig) { c.minute = expr }
}

// WithSecond sets the second field.
func WithSecond(expr string) CronOption {
	return func(c *cronConfig) { c.second = expr }
}

// WithCronStartDate sets the earliest trigger date.
func WithCronStartDate(t time.Time) CronOption {
	return func(c *cronConfig) { c.startDate = &t }
}

// WithCronEndDate sets the latest trigger date.
func WithCronEndDate(t time.Time) CronOption {
	return func(c *cronConfig) { c.endDate = &t }
}

// WithCronLocation sets the timezone.
func WithCronLocation(loc *time.Location) CronOption {
	return func(c *cronConfig) { c.location = loc }
}

// WithCronJitter sets the jitter.
func WithCronJitter(d time.Duration) CronOption {
	return func(c *cronConfig) { c.jitter = d }
}

// NewCronTrigger creates a new CronTrigger with the given options.
func NewCronTrigger(opts ...CronOption) (*CronTrigger, error) {
	cfg := &cronConfig{
		location: time.Local,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	t := &CronTrigger{
		StartDate: cfg.startDate,
		EndDate:   cfg.endDate,
		Location:  cfg.location,
		Jitter:    cfg.jitter,
	}

	// Build fields in order
	values := map[string]string{
		"year":        cfg.year,
		"month":       cfg.month,
		"day":         cfg.day,
		"week":        cfg.week,
		"day_of_week": cfg.dayOfWeek,
		"hour":        cfg.hour,
		"minute":      cfg.minute,
		"second":      cfg.second,
	}

	// Determine which fields are explicitly set
	hasExplicitValue := make(map[string]bool)
	for name, val := range values {
		if val != "" {
			hasExplicitValue[name] = true
		}
	}

	// Assign defaults after the last explicit value
	assignDefaults := false
	for _, fieldName := range FieldNames {
		var expr string
		var isDefault bool

		if values[fieldName] != "" {
			expr = values[fieldName]
			isDefault = false
			// After finding an explicit value, start assigning defaults to remaining fields
			assignDefaults = !hasRemainingExplicitValues(fieldName, values)
		} else if assignDefaults {
			expr = DefaultValues[fieldName]
			isDefault = true
		} else {
			expr = "*"
			isDefault = true
		}

		field, err := createField(fieldName, expr, isDefault)
		if err != nil {
			return nil, fmt.Errorf("error creating field %s: %w", fieldName, err)
		}
		t.Fields = append(t.Fields, field)
	}

	return t, nil
}

func hasRemainingExplicitValues(currentField string, values map[string]string) bool {
	found := false
	for _, name := range FieldNames {
		if name == currentField {
			found = true
			continue
		}
		if found && values[name] != "" {
			return true
		}
	}
	return false
}

func createField(name, expr string, isDefault bool) (Field, error) {
	switch name {
	case "month":
		return NewMonthField(expr, isDefault)
	case "day":
		return NewDayOfMonthField(expr, isDefault)
	case "week":
		return NewWeekField(expr, isDefault)
	case "day_of_week":
		return NewDayOfWeekField(expr, isDefault)
	default:
		return NewBaseField(name, expr, isDefault)
	}
}

// FromCrontab creates a CronTrigger from a standard 5-field crontab expression.
// Format: "minute hour day month day_of_week"
func FromCrontab(expr string, loc *time.Location) (*CronTrigger, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("wrong number of fields; got %d, expected 5", len(parts))
	}

	if loc == nil {
		loc = time.Local
	}

	return NewCronTrigger(
		WithMinute(parts[0]),
		WithHour(parts[1]),
		WithDay(parts[2]),
		WithMonth(parts[3]),
		WithDayOfWeek(parts[4]),
		WithCronLocation(loc),
	)
}

// GetNextFireTime calculates the next fire time.
func (t *CronTrigger) GetNextFireTime(previousFireTime *time.Time, now time.Time) *time.Time {
	var startDate time.Time

	if previousFireTime != nil {
		// Start from either now or just after previous fire time
		prevPlus := previousFireTime.Add(time.Microsecond)
		if prevPlus.Before(now) {
			startDate = now
		} else {
			startDate = prevPlus
		}
		if startDate.Equal(*previousFireTime) {
			startDate = startDate.Add(time.Microsecond)
		}
	} else {
		if t.StartDate != nil && t.StartDate.After(now) {
			startDate = *t.StartDate
		} else {
			startDate = now
		}
	}

	// Round up to next second
	nextDate := timeutil.Ceil(startDate).In(t.Location)

	fieldNum := 0
	for fieldNum >= 0 && fieldNum < len(t.Fields) {
		field := t.Fields[fieldNum]
		currValue := field.GetValue(nextDate)
		nextValue := field.GetNextValue(nextDate)

		if nextValue == nil {
			// No valid value found, increment previous field
			nextDate, fieldNum = t.incrementFieldValue(nextDate, fieldNum-1)
		} else if *nextValue > currValue {
			// A valid but higher value was found
			if field.IsReal() {
				nextDate = t.setFieldValue(nextDate, fieldNum, *nextValue)
				fieldNum++
			} else {
				nextDate, fieldNum = t.incrementFieldValue(nextDate, fieldNum)
			}
		} else {
			// Current value is valid
			fieldNum++
		}

		// Check end date
		if t.EndDate != nil && nextDate.After(*t.EndDate) {
			return nil
		}
	}

	if fieldNum >= 0 {
		result := t.applyJitter(nextDate, now)
		if t.EndDate != nil && result.After(*t.EndDate) {
			return t.EndDate
		}
		return &result
	}

	return nil
}

// incrementFieldValue increments the specified field and resets less significant fields.
func (t *CronTrigger) incrementFieldValue(date time.Time, fieldNum int) (time.Time, int) {
	values := make(map[string]int)

	i := 0
	for i < len(t.Fields) {
		field := t.Fields[i]
		if !field.IsReal() {
			if i == fieldNum {
				fieldNum--
				i--
			} else {
				i++
			}
			continue
		}

		if i < fieldNum {
			values[field.Name()] = field.GetValue(date)
			i++
		} else if i > fieldNum {
			values[field.Name()] = field.GetMin(date)
			i++
		} else {
			value := field.GetValue(date)
			maxVal := field.GetMax(date)
			if value == maxVal {
				fieldNum--
				i--
			} else {
				values[field.Name()] = value + 1
				i++
			}
		}
	}

	// Build new date from values
	year := getOrDefault(values, "year", date.Year())
	month := getOrDefault(values, "month", int(date.Month()))
	day := getOrDefault(values, "day", date.Day())
	hour := getOrDefault(values, "hour", date.Hour())
	minute := getOrDefault(values, "minute", date.Minute())
	second := getOrDefault(values, "second", date.Second())

	newDate := time.Date(year, time.Month(month), day, hour, minute, second, 0, t.Location)

	return newDate, fieldNum
}

// setFieldValue sets a specific field value and resets less significant fields.
func (t *CronTrigger) setFieldValue(date time.Time, fieldNum int, newValue int) time.Time {
	values := make(map[string]int)

	for i, field := range t.Fields {
		if !field.IsReal() {
			continue
		}

		if i < fieldNum {
			values[field.Name()] = field.GetValue(date)
		} else if i > fieldNum {
			values[field.Name()] = field.GetMin(date)
		} else {
			values[field.Name()] = newValue
		}
	}

	year := getOrDefault(values, "year", date.Year())
	month := getOrDefault(values, "month", int(date.Month()))
	day := getOrDefault(values, "day", date.Day())
	hour := getOrDefault(values, "hour", date.Hour())
	minute := getOrDefault(values, "minute", date.Minute())
	second := getOrDefault(values, "second", date.Second())

	return time.Date(year, time.Month(month), day, hour, minute, second, 0, t.Location)
}

func (t *CronTrigger) applyJitter(date time.Time, now time.Time) time.Time {
	if t.Jitter <= 0 {
		return date
	}

	// Use the trigger package's ApplyJitter via inline implementation
	// to avoid circular imports
	jitterNs := time.Duration(float64(t.Jitter) * float64(time.Now().UnixNano()%1000) / 1000.0)
	return date.Add(jitterNs)
}

func getOrDefault(m map[string]int, key string, defaultVal int) int {
	if val, ok := m[key]; ok {
		return val
	}
	return defaultVal
}

// String returns a string representation of the trigger.
func (t *CronTrigger) String() string {
	var parts []string
	for _, f := range t.Fields {
		if !f.IsDefault() {
			parts = append(parts, fmt.Sprintf("%s='%s'", f.Name(), f.String()))
		}
	}
	return fmt.Sprintf("cron[%s]", strings.Join(parts, ", "))
}

// GobEncode implements gob.GobEncoder for serialization.
func (t *CronTrigger) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	// Encode field expressions as strings
	fieldExprs := make([]string, len(t.Fields))
	fieldDefaults := make([]bool, len(t.Fields))
	for i, f := range t.Fields {
		fieldExprs[i] = f.String()
		fieldDefaults[i] = f.IsDefault()
	}

	if err := enc.Encode(fieldExprs); err != nil {
		return nil, err
	}
	if err := enc.Encode(fieldDefaults); err != nil {
		return nil, err
	}

	// Encode dates
	hasStart := t.StartDate != nil
	if err := enc.Encode(hasStart); err != nil {
		return nil, err
	}
	if hasStart {
		if err := enc.Encode(t.StartDate.Unix()); err != nil {
			return nil, err
		}
	}

	hasEnd := t.EndDate != nil
	if err := enc.Encode(hasEnd); err != nil {
		return nil, err
	}
	if hasEnd {
		if err := enc.Encode(t.EndDate.Unix()); err != nil {
			return nil, err
		}
	}

	// Encode location and jitter
	if err := enc.Encode(t.Location.String()); err != nil {
		return nil, err
	}
	if err := enc.Encode(int64(t.Jitter)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder for deserialization.
func (t *CronTrigger) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var fieldExprs []string
	var fieldDefaults []bool

	if err := dec.Decode(&fieldExprs); err != nil {
		return err
	}
	if err := dec.Decode(&fieldDefaults); err != nil {
		return err
	}

	// Recreate fields
	t.Fields = make([]Field, len(FieldNames))
	for i, name := range FieldNames {
		field, err := createField(name, fieldExprs[i], fieldDefaults[i])
		if err != nil {
			return err
		}
		t.Fields[i] = field
	}

	// Decode dates
	var hasStart bool
	if err := dec.Decode(&hasStart); err != nil {
		return err
	}
	if hasStart {
		var startSec int64
		if err := dec.Decode(&startSec); err != nil {
			return err
		}
		startDate := time.Unix(startSec, 0)
		t.StartDate = &startDate
	}

	var hasEnd bool
	if err := dec.Decode(&hasEnd); err != nil {
		return err
	}
	if hasEnd {
		var endSec int64
		if err := dec.Decode(&endSec); err != nil {
			return err
		}
		endDate := time.Unix(endSec, 0)
		t.EndDate = &endDate
	}

	// Decode location and jitter
	var locName string
	if err := dec.Decode(&locName); err != nil {
		return err
	}
	loc, err := time.LoadLocation(locName)
	if err != nil {
		loc = time.UTC
	}
	t.Location = loc

	var jitterNs int64
	if err := dec.Decode(&jitterNs); err != nil {
		return err
	}
	t.Jitter = time.Duration(jitterNs)

	return nil
}

func init() {
	gob.Register(&CronTrigger{})
}
