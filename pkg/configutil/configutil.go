package configutil

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

var DefaultSeverities = map[string]struct{}{
	"info":     {},
	"warning":  {},
	"critical": {},
}

func ParseClockHHMM(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, err
	}

	return parsed.Hour(), parsed.Minute(), nil
}

func ParseWeekday(value string) (time.Weekday, error) {
	normalized := strings.ToUpper(strings.TrimSpace(value))

	weekdays := map[string]time.Weekday{
		"SUN":       time.Sunday,
		"SUNDAY":    time.Sunday,
		"MON":       time.Monday,
		"MONDAY":    time.Monday,
		"TUE":       time.Tuesday,
		"TUESDAY":   time.Tuesday,
		"WED":       time.Wednesday,
		"WEDNESDAY": time.Wednesday,
		"THU":       time.Thursday,
		"THURSDAY":  time.Thursday,
		"FRI":       time.Friday,
		"FRIDAY":    time.Friday,
		"SAT":       time.Saturday,
		"SATURDAY":  time.Saturday,
	}

	weekday, ok := weekdays[normalized]
	if !ok {
		valid := []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}
		return 0, fmt.Errorf("expected one of %s", strings.Join(valid, ", "))
	}

	return weekday, nil
}

func NormalizeSeverity(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func ValidateSeverity(value string, allowed map[string]struct{}) error {
	if allowed == nil {
		allowed = DefaultSeverities
	}

	if _, ok := allowed[value]; ok {
		return nil
	}

	allowedValues := make([]string, 0, len(allowed))
	for severity := range allowed {
		allowedValues = append(allowedValues, severity)
	}
	sort.Strings(allowedValues)

	return fmt.Errorf("must be one of: %s", strings.Join(allowedValues, ", "))
}
