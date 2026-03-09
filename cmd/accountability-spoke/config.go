package main

import (
	"errors"
	"strings"
	"time"

	"personal-infrastructure/pkg/configutil"
	"personal-infrastructure/pkg/spokecontract"
)

type config struct {
	HTTPAddr           string
	DBPath             string
	ExpiryPollInterval time.Duration
	ExpiryGracePeriod  time.Duration
	ReminderInterval   time.Duration
	HubNotifyURL       string
	HubNotifyAuthToken string
	NotifyChannel      string
	NotifySeverity     string
	PolicyFile         string
	OpenAIBaseURL      string
	OpenAIAPIKey       string
	OpenAIModel        string
	OpenAITimeout      time.Duration
	DefaultSnooze      time.Duration
	MaxSnooze          time.Duration
	CommitCommandName  string
	ProofCommandName   string
	StatusCommandName  string
	CancelCommandName  string
	SnoozeCommandName  string
}

func loadConfig() (config, error) {
	var cfg config
	var err error

	cfg.HTTPAddr, err = configutil.StringEnvRequired("ACCOUNTABILITY_SPOKE_HTTP_ADDR")
	if err != nil {
		return config{}, err
	}

	cfg.DBPath, err = configutil.StringEnvRequired("ACCOUNTABILITY_DB_PATH")
	if err != nil {
		return config{}, err
	}

	cfg.ExpiryPollInterval, err = configutil.DurationEnvRequired("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL")
	if err != nil {
		return config{}, err
	}
	cfg.ExpiryGracePeriod, err = configutil.DurationEnvRequired("ACCOUNTABILITY_EXPIRY_GRACE_PERIOD")
	if err != nil {
		return config{}, err
	}
	cfg.ReminderInterval, err = configutil.DurationEnvRequired("ACCOUNTABILITY_REMINDER_INTERVAL")
	if err != nil {
		return config{}, err
	}
	cfg.DefaultSnooze, err = configutil.DurationEnvRequired("ACCOUNTABILITY_DEFAULT_SNOOZE")
	if err != nil {
		return config{}, err
	}
	cfg.MaxSnooze, err = configutil.DurationEnvRequired("ACCOUNTABILITY_MAX_SNOOZE")
	if err != nil {
		return config{}, err
	}
	notifyCfg, err := configutil.LoadNotifyConfig(
		"ACCOUNTABILITY_HUB_NOTIFY_URL",
		"ACCOUNTABILITY_HUB_NOTIFY_AUTH_TOKEN",
		"ACCOUNTABILITY_NOTIFY_CHANNEL",
		"ACCOUNTABILITY_NOTIFY_SEVERITY",
	)
	if err != nil {
		return config{}, err
	}
	cfg.HubNotifyURL = notifyCfg.URL
	cfg.HubNotifyAuthToken = notifyCfg.AuthToken
	cfg.NotifyChannel = notifyCfg.Channel
	cfg.NotifySeverity = notifyCfg.Severity

	cfg.PolicyFile, err = configutil.StringEnvRequired("ACCOUNTABILITY_POLICY_FILE")
	if err != nil {
		return config{}, err
	}
	cfg.OpenAIBaseURL = configutil.StringEnv("ACCOUNTABILITY_OPENAI_BASE_URL", "https://api.openai.com/v1")
	cfg.OpenAIAPIKey = configutil.StringEnv("ACCOUNTABILITY_OPENAI_API_KEY", "")
	cfg.OpenAIModel = configutil.StringEnv("ACCOUNTABILITY_OPENAI_MODEL", "gpt-4.1-mini")
	cfg.OpenAITimeout, err = configutil.DurationEnv("ACCOUNTABILITY_OPENAI_TIMEOUT", 20*time.Second)
	if err != nil {
		return config{}, err
	}

	cfg.CommitCommandName, err = requiredNormalizedCommand("ACCOUNTABILITY_COMMAND_COMMIT")
	if err != nil {
		return config{}, err
	}
	cfg.ProofCommandName, err = requiredNormalizedCommand("ACCOUNTABILITY_COMMAND_PROOF")
	if err != nil {
		return config{}, err
	}
	cfg.StatusCommandName, err = requiredNormalizedCommand("ACCOUNTABILITY_COMMAND_STATUS")
	if err != nil {
		return config{}, err
	}
	cfg.CancelCommandName, err = requiredNormalizedCommand("ACCOUNTABILITY_COMMAND_CANCEL")
	if err != nil {
		return config{}, err
	}
	cfg.SnoozeCommandName, err = requiredNormalizedCommand("ACCOUNTABILITY_COMMAND_SNOOZE")
	if err != nil {
		return config{}, err
	}

	if cfg.ExpiryPollInterval <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL must be positive")
	}
	if cfg.ExpiryGracePeriod < 0 {
		return config{}, errors.New("ACCOUNTABILITY_EXPIRY_GRACE_PERIOD must be zero or positive")
	}
	if cfg.ReminderInterval <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_REMINDER_INTERVAL must be positive")
	}
	if cfg.DefaultSnooze <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_DEFAULT_SNOOZE must be positive")
	}
	if cfg.MaxSnooze <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_MAX_SNOOZE must be positive")
	}
	if cfg.DefaultSnooze > cfg.MaxSnooze {
		return config{}, errors.New("ACCOUNTABILITY_DEFAULT_SNOOZE cannot exceed ACCOUNTABILITY_MAX_SNOOZE")
	}
	if strings.TrimSpace(cfg.PolicyFile) == "" {
		return config{}, errors.New("ACCOUNTABILITY_POLICY_FILE is required")
	}
	if cfg.OpenAITimeout <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_OPENAI_TIMEOUT must be positive")
	}
	return cfg, nil
}

func requiredNormalizedCommand(envKey string) (string, error) {
	value, err := configutil.StringEnvRequired(envKey)
	if err != nil {
		return "", err
	}

	return spokecontract.NormalizeCommandName(value), nil
}
