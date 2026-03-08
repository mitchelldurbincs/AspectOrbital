package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultFinanceHTTPAddr      = "127.0.0.1:8091"
	defaultFinanceHubNotifyURL  = "http://127.0.0.1:8080/notify"
	defaultFinanceChannel       = "finance-summary"
	defaultFinanceSeverity      = "info"
	defaultFinanceSummaryTitle  = "Weekly Subscription Summary"
	defaultSummaryWeekday       = "SUN"
	defaultSummaryTime          = "18:00"
	defaultSummaryTimezone      = "America/New_York"
	defaultSummaryLookbackDays  = 7
	defaultSummaryPollInterval  = 1 * time.Minute
	defaultSummaryHTTPTimeout   = 15 * time.Second
	defaultSummaryMaxItems      = 20
	defaultFinanceStateFilePath = "var/finance-spoke/state.json"
	defaultPlaidEnv             = "production"
)

type config struct {
	HTTPAddr string

	HubNotifyURL        string
	NotifyTargetChannel string
	NotifySeverity      string

	SummaryEnabled      bool
	SummaryTitle        string
	SummaryWeekday      time.Weekday
	SummaryHour         int
	SummaryMinute       int
	SummaryTimezone     string
	SummaryLookbackDays int
	SummarySendEmpty    bool
	SummaryMaxItems     int
	SummaryPollInterval time.Duration

	StateFilePath string

	PlaidClientID     string
	PlaidSecret       string
	PlaidEnvironment  string
	PlaidBaseURL      string
	PlaidAccessTokens []string
	PlaidClientName   string
	PlaidCountryCodes []string
	PlaidLanguage     string
	PlaidWebhookURL   string

	HTTPTimeout time.Duration
}

func loadConfig() (config, error) {
	var cfg config

	cfg.HTTPAddr = stringEnv("FINANCE_SPOKE_HTTP_ADDR", defaultFinanceHTTPAddr)

	cfg.HubNotifyURL = stringEnv("FINANCE_HUB_NOTIFY_URL", defaultFinanceHubNotifyURL)
	cfg.NotifyTargetChannel = stringEnv("FINANCE_NOTIFY_CHANNEL", defaultFinanceChannel)
	cfg.NotifySeverity = normalizeSeverity(stringEnv("FINANCE_NOTIFY_SEVERITY", defaultFinanceSeverity))

	cfg.SummaryEnabled = boolEnv("FINANCE_SUMMARY_ENABLED", false)
	cfg.SummaryTitle = stringEnv("FINANCE_SUMMARY_TITLE", defaultFinanceSummaryTitle)
	cfg.SummaryTimezone = stringEnv("FINANCE_SUMMARY_TIMEZONE", defaultSummaryTimezone)
	cfg.SummaryLookbackDays = intEnvWithDefault("FINANCE_SUMMARY_LOOKBACK_DAYS", defaultSummaryLookbackDays)
	cfg.SummarySendEmpty = boolEnv("FINANCE_SUMMARY_SEND_EMPTY", false)
	cfg.SummaryMaxItems = intEnvWithDefault("FINANCE_SUMMARY_MAX_ITEMS", defaultSummaryMaxItems)

	weekday, err := parseWeekday(stringEnv("FINANCE_SUMMARY_WEEKDAY", defaultSummaryWeekday))
	if err != nil {
		return config{}, fmt.Errorf("invalid FINANCE_SUMMARY_WEEKDAY: %w", err)
	}
	cfg.SummaryWeekday = weekday

	hour, minute, err := parseClock(stringEnv("FINANCE_SUMMARY_TIME", defaultSummaryTime))
	if err != nil {
		return config{}, fmt.Errorf("invalid FINANCE_SUMMARY_TIME: %w", err)
	}
	cfg.SummaryHour = hour
	cfg.SummaryMinute = minute

	cfg.SummaryPollInterval, err = durationEnv("FINANCE_SUMMARY_POLL_INTERVAL", defaultSummaryPollInterval)
	if err != nil {
		return config{}, err
	}

	cfg.StateFilePath = stringEnv("FINANCE_STATE_FILE", defaultFinanceStateFilePath)
	if !filepath.IsAbs(cfg.StateFilePath) {
		cfg.StateFilePath = filepath.Clean(cfg.StateFilePath)
	}

	cfg.PlaidClientID = strings.TrimSpace(os.Getenv("PLAID_CLIENT_ID"))
	cfg.PlaidSecret = strings.TrimSpace(os.Getenv("PLAID_SECRET"))
	cfg.PlaidEnvironment = strings.ToLower(strings.TrimSpace(stringEnv("PLAID_ENV", defaultPlaidEnv)))
	cfg.PlaidBaseURL, err = plaidBaseURLForEnvironment(cfg.PlaidEnvironment)
	if err != nil {
		return config{}, err
	}

	cfg.PlaidAccessTokens = parseCSVValues(os.Getenv("PLAID_ACCESS_TOKENS"))
	cfg.PlaidClientName = stringEnv("PLAID_CLIENT_NAME", "Aspect Orbital Finance")
	cfg.PlaidCountryCodes = parseCSVValues(stringEnv("PLAID_COUNTRY_CODES", "US"))
	cfg.PlaidLanguage = stringEnv("PLAID_LANGUAGE", "en")
	cfg.PlaidWebhookURL = strings.TrimSpace(os.Getenv("PLAID_WEBHOOK_URL"))

	cfg.HTTPTimeout, err = durationEnv("FINANCE_HTTP_TIMEOUT", defaultSummaryHTTPTimeout)
	if err != nil {
		return config{}, err
	}

	if err := validateConfig(cfg); err != nil {
		return config{}, err
	}

	return cfg, nil
}

func validateConfig(cfg config) error {
	if cfg.HubNotifyURL == "" {
		return errors.New("FINANCE_HUB_NOTIFY_URL is required")
	}
	if cfg.NotifyTargetChannel == "" {
		return errors.New("FINANCE_NOTIFY_CHANNEL is required")
	}
	if cfg.SummaryLookbackDays <= 0 {
		return errors.New("FINANCE_SUMMARY_LOOKBACK_DAYS must be positive")
	}
	if cfg.SummaryMaxItems <= 0 {
		return errors.New("FINANCE_SUMMARY_MAX_ITEMS must be positive")
	}
	if cfg.SummaryPollInterval <= 0 {
		return errors.New("FINANCE_SUMMARY_POLL_INTERVAL must be positive")
	}
	if cfg.HTTPTimeout <= 0 {
		return errors.New("FINANCE_HTTP_TIMEOUT must be positive")
	}

	allowedSeverities := map[string]struct{}{
		"info":     {},
		"warning":  {},
		"critical": {},
	}
	if _, ok := allowedSeverities[cfg.NotifySeverity]; !ok {
		return errors.New("FINANCE_NOTIFY_SEVERITY must be one of: info, warning, critical")
	}

	if _, err := time.LoadLocation(cfg.SummaryTimezone); err != nil {
		return fmt.Errorf("invalid FINANCE_SUMMARY_TIMEZONE: %w", err)
	}

	if cfg.SummaryEnabled {
		if cfg.PlaidClientID == "" {
			return errors.New("PLAID_CLIENT_ID is required when FINANCE_SUMMARY_ENABLED=true")
		}
		if cfg.PlaidSecret == "" {
			return errors.New("PLAID_SECRET is required when FINANCE_SUMMARY_ENABLED=true")
		}
		if len(cfg.PlaidAccessTokens) == 0 {
			return errors.New("PLAID_ACCESS_TOKENS must include at least one token when FINANCE_SUMMARY_ENABLED=true")
		}
	}
	if cfg.PlaidClientID != "" && cfg.PlaidSecret == "" {
		return errors.New("PLAID_SECRET is required when PLAID_CLIENT_ID is set")
	}
	if cfg.PlaidSecret != "" && cfg.PlaidClientID == "" {
		return errors.New("PLAID_CLIENT_ID is required when PLAID_SECRET is set")
	}
	if len(cfg.PlaidCountryCodes) == 0 {
		return errors.New("PLAID_COUNTRY_CODES must include at least one country code")
	}
	if cfg.PlaidLanguage == "" {
		return errors.New("PLAID_LANGUAGE cannot be empty")
	}

	return nil
}

func parseWeekday(value string) (time.Weekday, error) {
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
		valid := make([]string, 0, 7)
		for _, day := range []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"} {
			valid = append(valid, day)
		}
		return 0, fmt.Errorf("expected one of %s", strings.Join(valid, ", "))
	}

	return weekday, nil
}

func plaidBaseURLForEnvironment(environment string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "sandbox":
		return "https://sandbox.plaid.com", nil
	case "development":
		return "https://development.plaid.com", nil
	case "production":
		return "https://production.plaid.com", nil
	default:
		return "", fmt.Errorf("invalid PLAID_ENV %q; use sandbox, development, or production", environment)
	}
}

func parseCSVValues(raw string) []string {
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

func parseClock(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, err
	}

	return parsed.Hour(), parsed.Minute(), nil
}

func normalizeSeverity(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func boolEnv(key string, fallback bool) bool {
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

func stringEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
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

func intEnvWithDefault(key string, fallback int) int {
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
