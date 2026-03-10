package main

import (
	"errors"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	"personal-infrastructure/pkg/configutil"
	"personal-infrastructure/pkg/spokecontract"
)

type config struct {
	HTTPAddr              string        `envconfig:"ACCOUNTABILITY_SPOKE_HTTP_ADDR" required:"true"`
	DBPath                string        `envconfig:"ACCOUNTABILITY_DB_PATH" required:"true"`
	ExpiryPollInterval    time.Duration `envconfig:"ACCOUNTABILITY_EXPIRY_POLL_INTERVAL" required:"true"`
	ExpiryGracePeriod     time.Duration `envconfig:"ACCOUNTABILITY_EXPIRY_GRACE_PERIOD" required:"true"`
	ReminderInterval      time.Duration `envconfig:"ACCOUNTABILITY_REMINDER_INTERVAL" required:"true"`
	CheckInQuietPeriod    time.Duration `envconfig:"ACCOUNTABILITY_CHECKIN_QUIET_PERIOD" required:"true"`
	HubNotifyURL          string        `envconfig:"HUB_NOTIFY_URL" required:"true"`
	HubNotifyAuthToken    string        `envconfig:"HUB_NOTIFY_AUTH_TOKEN" required:"true"`
	SpokeCommandAuthToken string        `envconfig:"SPOKE_COMMAND_AUTH_TOKEN" required:"true"`
	DiscordCallbackURL    string        `envconfig:"ACCOUNTABILITY_DISCORD_CALLBACK_URL" required:"true"`
	CallbackAuthToken     string        `envconfig:"ACCOUNTABILITY_CALLBACK_AUTH_TOKEN" required:"true"`
	NotifyChannel         string        `envconfig:"ACCOUNTABILITY_NOTIFY_CHANNEL" required:"true"`
	NotifySeverity        string        `envconfig:"ACCOUNTABILITY_NOTIFY_SEVERITY" required:"true"`
	PolicyFile            string        `envconfig:"ACCOUNTABILITY_POLICY_FILE" required:"true"`
	OpenAIBaseURL         string        `envconfig:"ACCOUNTABILITY_OPENAI_BASE_URL" required:"true"`
	OpenAIAPIKey          string        `envconfig:"ACCOUNTABILITY_OPENAI_API_KEY"`
	OpenAIModel           string        `envconfig:"ACCOUNTABILITY_OPENAI_MODEL" required:"true"`
	OpenAITimeout         time.Duration `envconfig:"ACCOUNTABILITY_OPENAI_TIMEOUT" required:"true"`
	DefaultSnooze         time.Duration `envconfig:"ACCOUNTABILITY_DEFAULT_SNOOZE" required:"true"`
	MaxSnooze             time.Duration `envconfig:"ACCOUNTABILITY_MAX_SNOOZE" required:"true"`

	CommitCommandNameRaw  string `envconfig:"ACCOUNTABILITY_COMMAND_COMMIT" required:"true"`
	ProofCommandNameRaw   string `envconfig:"ACCOUNTABILITY_COMMAND_PROOF" required:"true"`
	StatusCommandNameRaw  string `envconfig:"ACCOUNTABILITY_COMMAND_STATUS" required:"true"`
	CancelCommandNameRaw  string `envconfig:"ACCOUNTABILITY_COMMAND_CANCEL" required:"true"`
	SnoozeCommandNameRaw  string `envconfig:"ACCOUNTABILITY_COMMAND_SNOOZE" required:"true"`
	CheckInCommandNameRaw string `envconfig:"ACCOUNTABILITY_COMMAND_CHECKIN" required:"true"`

	CommitCommandName  string `ignored:"true"`
	ProofCommandName   string `ignored:"true"`
	StatusCommandName  string `ignored:"true"`
	CancelCommandName  string `ignored:"true"`
	SnoozeCommandName  string `ignored:"true"`
	CheckInCommandName string `ignored:"true"`
}

func loadConfig() (config, error) {
	var cfg config
	if err := envconfig.Process("", &cfg); err != nil {
		return config{}, err
	}

	for key, value := range map[string]string{
		"ACCOUNTABILITY_SPOKE_HTTP_ADDR":      cfg.HTTPAddr,
		"ACCOUNTABILITY_DB_PATH":              cfg.DBPath,
		"HUB_NOTIFY_URL":                      cfg.HubNotifyURL,
		"HUB_NOTIFY_AUTH_TOKEN":               cfg.HubNotifyAuthToken,
		"SPOKE_COMMAND_AUTH_TOKEN":            cfg.SpokeCommandAuthToken,
		"ACCOUNTABILITY_DISCORD_CALLBACK_URL": cfg.DiscordCallbackURL,
		"ACCOUNTABILITY_CALLBACK_AUTH_TOKEN":  cfg.CallbackAuthToken,
		"ACCOUNTABILITY_NOTIFY_CHANNEL":       cfg.NotifyChannel,
		"ACCOUNTABILITY_NOTIFY_SEVERITY":      cfg.NotifySeverity,
		"ACCOUNTABILITY_POLICY_FILE":          cfg.PolicyFile,
		"ACCOUNTABILITY_COMMAND_COMMIT":       cfg.CommitCommandNameRaw,
		"ACCOUNTABILITY_COMMAND_PROOF":        cfg.ProofCommandNameRaw,
		"ACCOUNTABILITY_COMMAND_STATUS":       cfg.StatusCommandNameRaw,
		"ACCOUNTABILITY_COMMAND_CANCEL":       cfg.CancelCommandNameRaw,
		"ACCOUNTABILITY_COMMAND_SNOOZE":       cfg.SnoozeCommandNameRaw,
		"ACCOUNTABILITY_COMMAND_CHECKIN":      cfg.CheckInCommandNameRaw,
	} {
		if strings.TrimSpace(value) == "" {
			return config{}, errors.New(key + " is required")
		}
	}

	cfg.NotifySeverity = configutil.NormalizeSeverity(cfg.NotifySeverity)
	if err := configutil.ValidateSeverity(cfg.NotifySeverity, configutil.DefaultSeverities); err != nil {
		return config{}, err
	}

	cfg.CommitCommandName = spokecontract.NormalizeCommandName(cfg.CommitCommandNameRaw)
	cfg.ProofCommandName = spokecontract.NormalizeCommandName(cfg.ProofCommandNameRaw)
	cfg.StatusCommandName = spokecontract.NormalizeCommandName(cfg.StatusCommandNameRaw)
	cfg.CancelCommandName = spokecontract.NormalizeCommandName(cfg.CancelCommandNameRaw)
	cfg.SnoozeCommandName = spokecontract.NormalizeCommandName(cfg.SnoozeCommandNameRaw)
	cfg.CheckInCommandName = spokecontract.NormalizeCommandName(cfg.CheckInCommandNameRaw)
	if err := validateCommandNames(map[string]string{
		"ACCOUNTABILITY_COMMAND_COMMIT":  cfg.CommitCommandName,
		"ACCOUNTABILITY_COMMAND_PROOF":   cfg.ProofCommandName,
		"ACCOUNTABILITY_COMMAND_STATUS":  cfg.StatusCommandName,
		"ACCOUNTABILITY_COMMAND_CANCEL":  cfg.CancelCommandName,
		"ACCOUNTABILITY_COMMAND_SNOOZE":  cfg.SnoozeCommandName,
		"ACCOUNTABILITY_COMMAND_CHECKIN": cfg.CheckInCommandName,
	}); err != nil {
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
	if cfg.CheckInQuietPeriod <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_CHECKIN_QUIET_PERIOD must be positive")
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

func validateCommandNames(commands map[string]string) error {
	seen := make(map[string]string, len(commands))
	for envName, value := range commands {
		if err := spokecontract.ValidateCommandName(value); err != nil {
			return errors.New(envName + " is invalid after normalization: " + err.Error())
		}
		if prevEnv, ok := seen[value]; ok {
			return errors.New(prevEnv + " and " + envName + " both normalize to \"" + value + "\"")
		}
		seen[value] = envName
	}
	return nil
}
