package cron

import (
	"fmt"
	"strings"
	"time"
)

// Field bounds
var MinValues = map[string]int{
	"year":        1970,
	"month":       1,
	"day":         1,
	"week":        1,
	"day_of_week": 0,
	"hour":        0,
	"minute":      0,
	"second":      0,
}

var MaxValues = map[string]int{
	"year":        9999,
	"month":       12,
	"day":         31,
	"week":        53,
	"day_of_week": 6,
	"hour":        23,
	"minute":      59,
	"second":      59,
}

var DefaultValues = map[string]string{
	"year":        "*",
	"month":       "1",
	"day":         "1",
	"week":        "*",
	"day_of_week": "*",
	"hour":        "0",
	"minute":      "0",
	"second":      "0",
}

// Field represents a cron field (year, month, day, etc.).
type Field interface {
	// Name returns the field name.
	Name() string
	// IsReal returns true if this field directly maps to a datetime component.
	IsReal() bool
	// IsDefault returns true if this field has default value.
	IsDefault() bool
	// GetMin returns the minimum valid value for this field.
	GetMin(date time.Time) int
	// GetMax returns the maximum valid value for this field.
	GetMax(date time.Time) int
	// GetValue returns the current value of this field from the date.
	GetValue(date time.Time) int
	// GetNextValue returns the next valid value for this field.
	GetNextValue(date time.Time) *int
	// String returns a string representation.
	String() string
}

// BaseField is the default field implementation.
type BaseField struct {
	name        string
	expressions []Expression
	isDefault   bool
}

// NewBaseField creates a new field by parsing the expression.
func NewBaseField(name string, expr string, isDefault bool) (*BaseField, error) {
	f := &BaseField{
		name:      name,
		isDefault: isDefault,
	}

	if err := f.compileExpressions(expr); err != nil {
		return nil, err
	}

	return f, nil
}

func (f *BaseField) compileExpressions(exprs string) error {
	// Split comma-separated expressions
	parts := strings.Split(strings.TrimSpace(exprs), ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		expr, err := f.compileExpression(part)
		if err != nil {
			return err
		}
		f.expressions = append(f.expressions, expr)
	}

	if len(f.expressions) == 0 {
		return fmt.Errorf("no valid expressions found for field %s", f.name)
	}

	return nil
}

func (f *BaseField) compileExpression(expr string) (Expression, error) {
	// Try AllExpression first
	if e, ok := ParseAllExpression(expr); ok {
		return e, nil
	}

	// Try RangeExpression
	if e, ok := ParseRangeExpression(expr); ok {
		return e, nil
	}

	return nil, fmt.Errorf("unrecognized expression %q for field %q", expr, f.name)
}

func (f *BaseField) Name() string      { return f.name }
func (f *BaseField) IsReal() bool      { return true }
func (f *BaseField) IsDefault() bool   { return f.isDefault }
func (f *BaseField) GetMin(date time.Time) int { return MinValues[f.name] }
func (f *BaseField) GetMax(date time.Time) int { return MaxValues[f.name] }

func (f *BaseField) GetValue(date time.Time) int {
	switch f.name {
	case "year":
		return date.Year()
	case "month":
		return int(date.Month())
	case "day":
		return date.Day()
	case "hour":
		return date.Hour()
	case "minute":
		return date.Minute()
	case "second":
		return date.Second()
	default:
		return 0
	}
}

func (f *BaseField) GetNextValue(date time.Time) *int {
	var smallest *int
	for _, expr := range f.expressions {
		value := expr.GetNextValue(date, f)
		if value != nil && (smallest == nil || *value < *smallest) {
			smallest = value
		}
	}
	return smallest
}

func (f *BaseField) String() string {
	parts := make([]string, len(f.expressions))
	for i, expr := range f.expressions {
		parts[i] = expr.String()
	}
	return strings.Join(parts, ",")
}

// WeekField represents the ISO week field (not a "real" datetime field).
type WeekField struct {
	BaseField
}

// NewWeekField creates a new week field.
func NewWeekField(expr string, isDefault bool) (*WeekField, error) {
	base, err := NewBaseField("week", expr, isDefault)
	if err != nil {
		return nil, err
	}
	return &WeekField{BaseField: *base}, nil
}

func (f *WeekField) IsReal() bool { return false }

func (f *WeekField) GetValue(date time.Time) int {
	_, week := date.ISOWeek()
	return week
}

// DayOfMonthField represents the day of month field with special expressions.
type DayOfMonthField struct {
	BaseField
}

// NewDayOfMonthField creates a new day of month field.
func NewDayOfMonthField(expr string, isDefault bool) (*DayOfMonthField, error) {
	f := &DayOfMonthField{}
	f.name = "day"
	f.isDefault = isDefault

	if err := f.compileExpressions(expr); err != nil {
		return nil, err
	}

	return f, nil
}

func (f *DayOfMonthField) compileExpressions(exprs string) error {
	parts := strings.Split(strings.TrimSpace(exprs), ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		expr, err := f.compileExpression(part)
		if err != nil {
			return err
		}
		f.expressions = append(f.expressions, expr)
	}

	if len(f.expressions) == 0 {
		return fmt.Errorf("no valid expressions found for field day")
	}

	return nil
}

func (f *DayOfMonthField) compileExpression(expr string) (Expression, error) {
	// Try WeekdayPositionExpression first (e.g., "1st mon")
	if e, ok := ParseWeekdayPositionExpression(expr); ok {
		return e, nil
	}

	// Try LastDayOfMonthExpression
	if e, ok := ParseLastDayOfMonthExpression(expr); ok {
		return e, nil
	}

	// Try AllExpression
	if e, ok := ParseAllExpression(expr); ok {
		return e, nil
	}

	// Try RangeExpression
	if e, ok := ParseRangeExpression(expr); ok {
		return e, nil
	}

	return nil, fmt.Errorf("unrecognized expression %q for field day", expr)
}

func (f *DayOfMonthField) GetMax(date time.Time) int {
	return daysInMonth(date.Year(), date.Month())
}

// DayOfWeekField represents the day of week field (not a "real" datetime field).
type DayOfWeekField struct {
	BaseField
}

// NewDayOfWeekField creates a new day of week field.
func NewDayOfWeekField(expr string, isDefault bool) (*DayOfWeekField, error) {
	f := &DayOfWeekField{}
	f.name = "day_of_week"
	f.isDefault = isDefault

	if err := f.compileExpressions(expr); err != nil {
		return nil, err
	}

	return f, nil
}

func (f *DayOfWeekField) compileExpressions(exprs string) error {
	parts := strings.Split(strings.TrimSpace(exprs), ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		expr, err := f.compileExpression(part)
		if err != nil {
			return err
		}
		f.expressions = append(f.expressions, expr)
	}

	if len(f.expressions) == 0 {
		return fmt.Errorf("no valid expressions found for field day_of_week")
	}

	return nil
}

func (f *DayOfWeekField) compileExpression(expr string) (Expression, error) {
	// Try WeekdayRangeExpression first (e.g., "mon-fri")
	if e, ok := ParseWeekdayRangeExpression(expr); ok {
		return e, nil
	}

	// Try AllExpression
	if e, ok := ParseAllExpression(expr); ok {
		return e, nil
	}

	// Try RangeExpression
	if e, ok := ParseRangeExpression(expr); ok {
		return e, nil
	}

	return nil, fmt.Errorf("unrecognized expression %q for field day_of_week", expr)
}

func (f *DayOfWeekField) IsReal() bool { return false }

func (f *DayOfWeekField) GetValue(date time.Time) int {
	// Go: Sunday = 0, we want Monday = 0
	wd := int(date.Weekday())
	if wd == 0 {
		return 6 // Sunday
	}
	return wd - 1
}

// MonthField represents the month field with name support.
type MonthField struct {
	BaseField
}

// NewMonthField creates a new month field.
func NewMonthField(expr string, isDefault bool) (*MonthField, error) {
	f := &MonthField{}
	f.name = "month"
	f.isDefault = isDefault

	if err := f.compileExpressions(expr); err != nil {
		return nil, err
	}

	return f, nil
}

func (f *MonthField) compileExpressions(exprs string) error {
	parts := strings.Split(strings.TrimSpace(exprs), ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		expr, err := f.compileExpression(part)
		if err != nil {
			return err
		}
		f.expressions = append(f.expressions, expr)
	}

	if len(f.expressions) == 0 {
		return fmt.Errorf("no valid expressions found for field month")
	}

	return nil
}

func (f *MonthField) compileExpression(expr string) (Expression, error) {
	// Try MonthRangeExpression first (e.g., "jan-mar")
	if e, ok := ParseMonthRangeExpression(expr); ok {
		return e, nil
	}

	// Try AllExpression
	if e, ok := ParseAllExpression(expr); ok {
		return e, nil
	}

	// Try RangeExpression
	if e, ok := ParseRangeExpression(expr); ok {
		return e, nil
	}

	return nil, fmt.Errorf("unrecognized expression %q for field month", expr)
}
