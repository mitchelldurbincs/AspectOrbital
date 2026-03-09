package configutil

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var DefaultSeverities = map[string]struct{}{
	"info":     {},
	"warning":  {},
	"critical": {},
}

func StringEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func StringEnvRequired(key string) (string, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return "", fmt.Errorf("%s is required", key)
	}

	return raw, nil
}

func DurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}

	return value, nil
}

func DurationEnvRequired(key string) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, fmt.Errorf("%s is required", key)
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}

	return value, nil
}

func IntEnvDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}

func IntEnvWithDefaultStrict(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}

	return value, nil
}

func IntEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}

	return value, nil
}

func IntEnvRequired(key string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, fmt.Errorf("%s is required", key)
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}

	return value, nil
}

func BoolEnvDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}

	return value
}

func BoolEnvWithDefaultStrict(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}

	return value, nil
}

func BoolEnvRequired(key string) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, fmt.Errorf("%s is required", key)
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}

	return value, nil
}

func ParseClockHHMM(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, err
	}

	return parsed.Hour(), parsed.Minute(), nil
}

func ParseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))

	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		values = append(values, value)
	}

	return values
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

type NotifyConfig struct {
	URL       string
	AuthToken string
	Channel   string
	Severity  string
}

func LoadNotifyConfig(urlKey, authTokenKey, channelKey, severityKey string) (NotifyConfig, error) {
	urlValue, err := StringEnvRequired(urlKey)
	if err != nil {
		return NotifyConfig{}, err
	}

	authToken, err := StringEnvRequired(authTokenKey)
	if err != nil {
		return NotifyConfig{}, err
	}

	channel, err := StringEnvRequired(channelKey)
	if err != nil {
		return NotifyConfig{}, err
	}

	severityRaw, err := StringEnvRequired(severityKey)
	if err != nil {
		return NotifyConfig{}, err
	}
	severity := NormalizeSeverity(severityRaw)
	if err := ValidateSeverity(severity, DefaultSeverities); err != nil {
		return NotifyConfig{}, fmt.Errorf("%s %w", severityKey, err)
	}

	return NotifyConfig{
		URL:       urlValue,
		AuthToken: authToken,
		Channel:   channel,
		Severity:  severity,
	}, nil
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
