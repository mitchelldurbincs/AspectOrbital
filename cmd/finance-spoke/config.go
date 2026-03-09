package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	"personal-infrastructure/pkg/configutil"
)

type config struct {
	HTTPAddr string `envconfig:"FINANCE_SPOKE_HTTP_ADDR" required:"true"`

	HubNotifyURL        string `envconfig:"FINANCE_HUB_NOTIFY_URL" required:"true"`
	HubNotifyAuthToken  string `envconfig:"FINANCE_HUB_NOTIFY_AUTH_TOKEN" required:"true"`
	NotifyTargetChannel string `envconfig:"FINANCE_NOTIFY_CHANNEL" required:"true"`
	NotifySeverity      string `envconfig:"FINANCE_NOTIFY_SEVERITY" required:"true"`

	SummaryEnabled      bool          `envconfig:"FINANCE_SUMMARY_ENABLED" required:"true"`
	SummaryTitle        string        `envconfig:"FINANCE_SUMMARY_TITLE" required:"true"`
	SummaryTimezone     string        `envconfig:"FINANCE_SUMMARY_TIMEZONE" required:"true"`
	SummaryLookbackDays int           `envconfig:"FINANCE_SUMMARY_LOOKBACK_DAYS" required:"true"`
	SummarySendEmpty    bool          `envconfig:"FINANCE_SUMMARY_SEND_EMPTY" required:"true"`
	SummaryMaxItems     int           `envconfig:"FINANCE_SUMMARY_MAX_ITEMS" required:"true"`
	SummaryPollInterval time.Duration `envconfig:"FINANCE_SUMMARY_POLL_INTERVAL" required:"true"`

	SummaryWeekdayRaw string       `envconfig:"FINANCE_SUMMARY_WEEKDAY" required:"true"`
	SummaryWeekday    time.Weekday `ignored:"true"`

	SummaryTimeRaw string `envconfig:"FINANCE_SUMMARY_TIME" required:"true"`
	SummaryHour    int    `ignored:"true"`
	SummaryMinute  int    `ignored:"true"`

	StateFilePath string `envconfig:"FINANCE_STATE_FILE" required:"true"`

	PlaidClientID     string   `envconfig:"PLAID_CLIENT_ID" required:"true"`
	PlaidSecret       string   `envconfig:"PLAID_SECRET" required:"true"`
	PlaidEnvironment  string   `envconfig:"PLAID_ENV" required:"true"`
	PlaidBaseURL      string   `ignored:"true"`
	PlaidAccessTokens []string `envconfig:"PLAID_ACCESS_TOKENS" required:"true"`
	PlaidClientName   string   `envconfig:"PLAID_CLIENT_NAME" required:"true"`
	PlaidCountryCodes []string `envconfig:"PLAID_COUNTRY_CODES" required:"true"`
	PlaidLanguage     string   `envconfig:"PLAID_LANGUAGE" required:"true"`
	PlaidWebhookURL   string   `envconfig:"PLAID_WEBHOOK_URL" required:"true"`

	HTTPTimeout time.Duration `envconfig:"FINANCE_HTTP_TIMEOUT" required:"true"`
}

func loadConfig() (config, error) {
	var cfg config
	if err := envconfig.Process("", &cfg); err != nil {
		return config{}, err
	}

	cfg.NotifySeverity = configutil.NormalizeSeverity(cfg.NotifySeverity)
	if err := configutil.ValidateSeverity(cfg.NotifySeverity, configutil.DefaultSeverities); err != nil {
		return config{}, err
	}

	weekday, err := configutil.ParseWeekday(cfg.SummaryWeekdayRaw)
	if err != nil {
		return config{}, fmt.Errorf("invalid FINANCE_SUMMARY_WEEKDAY: %w", err)
	}
	cfg.SummaryWeekday = weekday

	hour, minute, err := configutil.ParseClockHHMM(cfg.SummaryTimeRaw)
	if err != nil {
		return config{}, fmt.Errorf("invalid FINANCE_SUMMARY_TIME: %w", err)
	}
	cfg.SummaryHour = hour
	cfg.SummaryMinute = minute

	if !filepath.IsAbs(cfg.StateFilePath) {
		cfg.StateFilePath = filepath.Clean(cfg.StateFilePath)
	}

	cfg.PlaidEnvironment = strings.ToLower(strings.TrimSpace(cfg.PlaidEnvironment))
	cfg.PlaidBaseURL, err = plaidBaseURLForEnvironment(cfg.PlaidEnvironment)
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
