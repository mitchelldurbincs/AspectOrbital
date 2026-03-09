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

	notifyCfg, err := configutil.LoadNotifyConfig(
		"FINANCE_HUB_NOTIFY_URL",
		"FINANCE_HUB_NOTIFY_AUTH_TOKEN",
		"FINANCE_NOTIFY_CHANNEL",
		"FINANCE_NOTIFY_SEVERITY",
	)
	if err != nil {
		return config{}, err
	}
	cfg.HubNotifyURL = notifyCfg.URL
	cfg.HubNotifyAuthToken = notifyCfg.AuthToken
	cfg.NotifyTargetChannel = notifyCfg.Channel
	cfg.NotifySeverity = notifyCfg.Severity

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
	weekday, err := configutil.ParseWeekday(summaryWeekday)
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
	cfg.PlaidAccessTokens = configutil.ParseCSV(plaidAccessTokens)
	cfg.PlaidClientName, err = configutil.StringEnvRequired("PLAID_CLIENT_NAME")
	if err != nil {
		return config{}, err
	}
	plaidCountryCodes, err := configutil.StringEnvRequired("PLAID_COUNTRY_CODES")
	if err != nil {
		return config{}, err
	}
	cfg.PlaidCountryCodes = configutil.ParseCSV(plaidCountryCodes)
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
