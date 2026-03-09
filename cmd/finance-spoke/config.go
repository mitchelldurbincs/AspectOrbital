package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"personal-infrastructure/pkg/configutil"
)

type config struct {
	HTTPAddr string

	HubNotifyURL        string
	HubNotifyAuthToken  string
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
	var err error

	cfg.HTTPAddr, err = configutil.StringEnvRequired("FINANCE_SPOKE_HTTP_ADDR")
	if err != nil {
		return config{}, err
	}

	cfg.HubNotifyURL, err = configutil.StringEnvRequired("FINANCE_HUB_NOTIFY_URL")
	if err != nil {
		return config{}, err
	}
	cfg.HubNotifyAuthToken, err = configutil.StringEnvRequired("FINANCE_HUB_NOTIFY_AUTH_TOKEN")
	if err != nil {
		return config{}, err
	}
	cfg.NotifyTargetChannel, err = configutil.StringEnvRequired("FINANCE_NOTIFY_CHANNEL")
	if err != nil {
		return config{}, err
	}
	notifySeverity, err := configutil.StringEnvRequired("FINANCE_NOTIFY_SEVERITY")
	if err != nil {
		return config{}, err
	}
	cfg.NotifySeverity = configutil.NormalizeSeverity(notifySeverity)

	cfg.SummaryEnabled, err = configutil.BoolEnvRequired("FINANCE_SUMMARY_ENABLED")
	if err != nil {
		return config{}, err
	}
	cfg.SummaryTitle, err = configutil.StringEnvRequired("FINANCE_SUMMARY_TITLE")
	if err != nil {
		return config{}, err
	}
	cfg.SummaryTimezone, err = configutil.StringEnvRequired("FINANCE_SUMMARY_TIMEZONE")
	if err != nil {
		return config{}, err
	}
	cfg.SummaryLookbackDays, err = configutil.IntEnvRequired("FINANCE_SUMMARY_LOOKBACK_DAYS")
	if err != nil {
		return config{}, err
	}
	cfg.SummarySendEmpty, err = configutil.BoolEnvRequired("FINANCE_SUMMARY_SEND_EMPTY")
	if err != nil {
		return config{}, err
	}
	cfg.SummaryMaxItems, err = configutil.IntEnvRequired("FINANCE_SUMMARY_MAX_ITEMS")
	if err != nil {
		return config{}, err
	}

	summaryWeekday, err := configutil.StringEnvRequired("FINANCE_SUMMARY_WEEKDAY")
	if err != nil {
		return config{}, err
	}
	weekday, err := parseWeekday(summaryWeekday)
	if err != nil {
		return config{}, fmt.Errorf("invalid FINANCE_SUMMARY_WEEKDAY: %w", err)
	}
	cfg.SummaryWeekday = weekday

	summaryTime, err := configutil.StringEnvRequired("FINANCE_SUMMARY_TIME")
	if err != nil {
		return config{}, err
	}
	hour, minute, err := configutil.ParseClockHHMM(summaryTime)
	if err != nil {
		return config{}, fmt.Errorf("invalid FINANCE_SUMMARY_TIME: %w", err)
	}
	cfg.SummaryHour = hour
	cfg.SummaryMinute = minute

	cfg.SummaryPollInterval, err = configutil.DurationEnvRequired("FINANCE_SUMMARY_POLL_INTERVAL")
	if err != nil {
		return config{}, err
	}

	cfg.StateFilePath, err = configutil.StringEnvRequired("FINANCE_STATE_FILE")
	if err != nil {
		return config{}, err
	}
	if !filepath.IsAbs(cfg.StateFilePath) {
		cfg.StateFilePath = filepath.Clean(cfg.StateFilePath)
	}

	cfg.PlaidClientID, err = configutil.StringEnvRequired("PLAID_CLIENT_ID")
	if err != nil {
		return config{}, err
	}
	cfg.PlaidSecret, err = configutil.StringEnvRequired("PLAID_SECRET")
	if err != nil {
		return config{}, err
	}
	plaidEnv, err := configutil.StringEnvRequired("PLAID_ENV")
	if err != nil {
		return config{}, err
	}
	cfg.PlaidEnvironment = strings.ToLower(strings.TrimSpace(plaidEnv))
	cfg.PlaidBaseURL, err = plaidBaseURLForEnvironment(cfg.PlaidEnvironment)
	if err != nil {
		return config{}, err
	}

	plaidAccessTokens, err := configutil.StringEnvRequired("PLAID_ACCESS_TOKENS")
	if err != nil {
		return config{}, err
	}
	cfg.PlaidAccessTokens = parseCSVValues(plaidAccessTokens)
	cfg.PlaidClientName, err = configutil.StringEnvRequired("PLAID_CLIENT_NAME")
	if err != nil {
		return config{}, err
	}
	plaidCountryCodes, err := configutil.StringEnvRequired("PLAID_COUNTRY_CODES")
	if err != nil {
		return config{}, err
	}
	cfg.PlaidCountryCodes = parseCSVValues(plaidCountryCodes)
	cfg.PlaidLanguage, err = configutil.StringEnvRequired("PLAID_LANGUAGE")
	if err != nil {
		return config{}, err
	}
	cfg.PlaidWebhookURL, err = configutil.StringEnvRequired("PLAID_WEBHOOK_URL")
	if err != nil {
		return config{}, err
	}

	cfg.HTTPTimeout, err = configutil.DurationEnvRequired("FINANCE_HTTP_TIMEOUT")
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
	if cfg.HubNotifyAuthToken == "" {
		return errors.New("FINANCE_HUB_NOTIFY_AUTH_TOKEN is required")
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

	if err := configutil.ValidateSeverity(cfg.NotifySeverity, configutil.DefaultSeverities); err != nil {
		return fmt.Errorf("FINANCE_NOTIFY_SEVERITY %w", err)
	}

	if _, err := time.LoadLocation(cfg.SummaryTimezone); err != nil {
		return fmt.Errorf("invalid FINANCE_SUMMARY_TIMEZONE: %w", err)
	}

	if len(cfg.PlaidAccessTokens) == 0 {
		return errors.New("PLAID_ACCESS_TOKENS must include at least one token")
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
