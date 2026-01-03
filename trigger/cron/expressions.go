// Package cron provides a cron-style trigger for scheduling jobs.
package cron

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Weekday names for parsing
var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// Month names for parsing
var Months = []string{
	"jan", "feb", "mar", "apr", "may", "jun",
	"jul", "aug", "sep", "oct", "nov", "dec",
}

// Expression represents a cron expression component.
type Expression interface {
	// GetNextValue returns the next valid value for this expression.
	GetNextValue(date time.Time, field Field) *int
	// String returns a string representation.
	String() string
}

// AllExpression matches all values with optional step.
// Pattern: * or */step
type AllExpression struct {
	Step *int
}

var allExprRe = regexp.MustCompile(`^\*(?:/(\d+))?$`)

// ParseAllExpression parses an all expression like "*" or "*/5".
func ParseAllExpression(expr string) (*AllExpression, bool) {
	match := allExprRe.FindStringSubmatch(expr)
	if match == nil {
		return nil, false
	}

	e := &AllExpression{}
	if match[1] != "" {
		step, _ := strconv.Atoi(match[1])
		if step == 0 {
			return nil, false // Step must be > 0
		}
		e.Step = &step
	}
	return e, true
}

// GetNextValue returns the next value matching this expression.
func (e *AllExpression) GetNextValue(date time.Time, field Field) *int {
	start := field.GetValue(date)
	minVal := field.GetMin(date)
	maxVal := field.GetMax(date)

	if start < minVal {
		start = minVal
	}

	var next int
	if e.Step == nil {
		next = start
	} else {
		distanceToNext := (*e.Step - (start - minVal) % *e.Step) % *e.Step
		next = start + distanceToNext
	}

	if next <= maxVal {
		return &next
	}
	return nil
}

func (e *AllExpression) String() string {
	if e.Step != nil {
		return fmt.Sprintf("*/%d", *e.Step)
	}
	return "*"
}

// RangeExpression matches a range of values with optional step.
// Pattern: first, first-last, or first-last/step
type RangeExpression struct {
	First int
	Last  *int
	Step  *int
}

var rangeExprRe = regexp.MustCompile(`^(\d+)(?:-(\d+))?(?:/(\d+))?$`)

// ParseRangeExpression parses a range expression like "1", "1-5", or "1-10/2".
func ParseRangeExpression(expr string) (*RangeExpression, bool) {
	match := rangeExprRe.FindStringSubmatch(expr)
	if match == nil {
		return nil, false
	}

	e := &RangeExpression{}
	e.First, _ = strconv.Atoi(match[1])

	if match[2] != "" {
		last, _ := strconv.Atoi(match[2])
		e.Last = &last
	}

	if match[3] != "" {
		step, _ := strconv.Atoi(match[3])
		if step == 0 {
			return nil, false
		}
		e.Step = &step
	}

	// If no last and no step, last = first (single value)
	if e.Last == nil && e.Step == nil {
		e.Last = &e.First
	}

	// Validate first <= last
	if e.Last != nil && e.First > *e.Last {
		return nil, false
	}

	return e, true
}

// GetNextValue returns the next value matching this expression.
func (e *RangeExpression) GetNextValue(date time.Time, field Field) *int {
	startVal := field.GetValue(date)
	minVal := field.GetMin(date)
	maxVal := field.GetMax(date)

	// Apply range limits
	if minVal < e.First {
		minVal = e.First
	}
	if e.Last != nil && maxVal > *e.Last {
		maxVal = *e.Last
	}

	nextVal := startVal
	if nextVal < minVal {
		nextVal = minVal
	}

	// Apply step if defined
	if e.Step != nil {
		distanceToNext := (*e.Step - (nextVal - minVal) % *e.Step) % *e.Step
		nextVal += distanceToNext
	}

	if nextVal <= maxVal {
		return &nextVal
	}
	return nil
}

func (e *RangeExpression) String() string {
	var s string
	if e.Last != nil && *e.Last != e.First {
		s = fmt.Sprintf("%d-%d", e.First, *e.Last)
	} else {
		s = strconv.Itoa(e.First)
	}

	if e.Step != nil {
		s += fmt.Sprintf("/%d", *e.Step)
	}
	return s
}

// MonthRangeExpression matches a range of months by name.
// Pattern: jan, jan-mar
type MonthRangeExpression struct {
	RangeExpression
}

var monthRangeExprRe = regexp.MustCompile(`^([a-zA-Z]+)(?:-([a-zA-Z]+))?$`)

// ParseMonthRangeExpression parses a month range expression.
func ParseMonthRangeExpression(expr string) (*MonthRangeExpression, bool) {
	match := monthRangeExprRe.FindStringSubmatch(expr)
	if match == nil {
		return nil, false
	}

	firstIdx := indexOf(Months, strings.ToLower(match[1]))
	if firstIdx < 0 {
		return nil, false
	}

	e := &MonthRangeExpression{}
	e.First = firstIdx + 1 // Months are 1-indexed

	if match[2] != "" {
		lastIdx := indexOf(Months, strings.ToLower(match[2]))
		if lastIdx < 0 {
			return nil, false
		}
		last := lastIdx + 1
		e.Last = &last
	}

	return e, true
}

func (e *MonthRangeExpression) String() string {
	if e.Last != nil && *e.Last != e.First {
		return fmt.Sprintf("%s-%s", Months[e.First-1], Months[*e.Last-1])
	}
	return Months[e.First-1]
}

// WeekdayRangeExpression matches a range of weekdays by name.
// Pattern: mon, mon-fri
type WeekdayRangeExpression struct {
	RangeExpression
}

var weekdayRangeExprRe = regexp.MustCompile(`^([a-zA-Z]+)(?:-([a-zA-Z]+))?$`)

// ParseWeekdayRangeExpression parses a weekday range expression.
func ParseWeekdayRangeExpression(expr string) (*WeekdayRangeExpression, bool) {
	match := weekdayRangeExprRe.FindStringSubmatch(expr)
	if match == nil {
		return nil, false
	}

	firstIdx := indexOf(Weekdays, strings.ToLower(match[1]))
	if firstIdx < 0 {
		return nil, false
	}

	e := &WeekdayRangeExpression{}
	e.First = firstIdx

	if match[2] != "" {
		lastIdx := indexOf(Weekdays, strings.ToLower(match[2]))
		if lastIdx < 0 {
			return nil, false
		}
		e.Last = &lastIdx
	}

	return e, true
}

func (e *WeekdayRangeExpression) String() string {
	if e.Last != nil && *e.Last != e.First {
		return fmt.Sprintf("%s-%s", Weekdays[e.First], Weekdays[*e.Last])
	}
	return Weekdays[e.First]
}

// WeekdayPositionExpression matches a specific weekday of the month.
// Pattern: "1st mon", "2nd tue", "last fri"
type WeekdayPositionExpression struct {
	OptionNum int // 0-4 for 1st-5th, 5 for last
	Weekday   int // 0-6 for mon-sun
}

var weekdayPositionOptions = []string{"1st", "2nd", "3rd", "4th", "5th", "last"}
var weekdayPositionRe = regexp.MustCompile(`^(1st|2nd|3rd|4th|5th|last)\s+(\w+)$`)

// ParseWeekdayPositionExpression parses a weekday position expression.
func ParseWeekdayPositionExpression(expr string) (*WeekdayPositionExpression, bool) {
	match := weekdayPositionRe.FindStringSubmatch(strings.ToLower(expr))
	if match == nil {
		return nil, false
	}

	optionIdx := indexOf(weekdayPositionOptions, match[1])
	if optionIdx < 0 {
		return nil, false
	}

	weekdayIdx := indexOf(Weekdays, match[2])
	if weekdayIdx < 0 {
		return nil, false
	}

	return &WeekdayPositionExpression{
		OptionNum: optionIdx,
		Weekday:   weekdayIdx,
	}, true
}

// GetNextValue returns the next matching day of month.
func (e *WeekdayPositionExpression) GetNextValue(date time.Time, field Field) *int {
	// Get the first day of the month and days in month
	firstOfMonth := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	firstDayWday := int(firstOfMonth.Weekday())
	// Convert Sunday=0 to our format where Monday=0
	if firstDayWday == 0 {
		firstDayWday = 6
	} else {
		firstDayWday--
	}

	lastDay := daysInMonth(date.Year(), date.Month())

	// Calculate first occurrence of target weekday
	firstHitDay := e.Weekday - firstDayWday + 1
	if firstHitDay <= 0 {
		firstHitDay += 7
	}

	// Calculate target day
	var targetDay int
	if e.OptionNum < 5 {
		targetDay = firstHitDay + e.OptionNum*7
	} else {
		// "last" - find the last occurrence
		targetDay = firstHitDay + ((lastDay - firstHitDay) / 7) * 7
	}

	if targetDay <= lastDay && targetDay >= date.Day() {
		return &targetDay
	}
	return nil
}

func (e *WeekdayPositionExpression) String() string {
	return fmt.Sprintf("%s %s", weekdayPositionOptions[e.OptionNum], Weekdays[e.Weekday])
}

// LastDayOfMonthExpression matches the last day of the month.
// Pattern: last
type LastDayOfMonthExpression struct{}

var lastDayExprRe = regexp.MustCompile(`^last$`)

// ParseLastDayOfMonthExpression parses a "last" expression.
func ParseLastDayOfMonthExpression(expr string) (*LastDayOfMonthExpression, bool) {
	if !lastDayExprRe.MatchString(strings.ToLower(expr)) {
		return nil, false
	}
	return &LastDayOfMonthExpression{}, true
}

// GetNextValue returns the last day of the month.
func (e *LastDayOfMonthExpression) GetNextValue(date time.Time, field Field) *int {
	lastDay := daysInMonth(date.Year(), date.Month())
	return &lastDay
}

func (e *LastDayOfMonthExpression) String() string {
	return "last"
}

// Helper functions

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
