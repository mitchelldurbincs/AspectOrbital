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

func ParseClockHHMM(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, err
	}

	return parsed.Hour(), parsed.Minute(), nil
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
