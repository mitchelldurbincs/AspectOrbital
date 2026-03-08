package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"personal-infrastructure/pkg/configutil"
)

const (
	defaultHTTPAddr          = "127.0.0.1:8090"
	defaultBeeminderBaseURL  = "https://www.beeminder.com/api/v1"
	defaultHubNotifyURL      = "http://127.0.0.1:8080/notify"
	defaultTargetChannel     = "mandarin-streaks"
	defaultNotifySeverity    = "critical"
	defaultPollInterval      = 1 * time.Minute
	defaultReminderInterval  = 5 * time.Minute
	defaultReminderStart     = "19:00"
	defaultActiveGrace       = 20 * time.Minute
	defaultStartedSnooze     = 30 * time.Minute
	defaultSnoozeDuration    = 30 * time.Minute
	defaultHTTPTimeout       = 10 * time.Second
	defaultDatapointsPerPage = 100
	defaultMaxDatapointPages = 20
)

type config struct {
	HTTPAddr string

	BeeminderBaseURL   string
	BeeminderAuthToken string
	BeeminderUsername  string
	BeeminderGoalSlug  string

	HubNotifyURL        string
	NotifyTargetChannel string
	NotifySeverity      string

	PollInterval        time.Duration
	ReminderInterval    time.Duration
	ReminderStartHour   int
	ReminderStartMinute int

	ActiveGrace   time.Duration
	StartedSnooze time.Duration
	DefaultSnooze time.Duration

	HTTPTimeout       time.Duration
	DatapointsPerPage int
	MaxDatapointPages int

	Commands controlCommands
}

type controlCommands struct {
	Started string
	Snooze  string
	Resume  string
	Status  string
}

func (c controlCommands) All() []string {
	commands := []string{c.Started, c.Snooze, c.Resume, c.Status}
	seen := make(map[string]struct{}, len(commands))
	unique := make([]string, 0, len(commands))

	for _, command := range commands {
		if command == "" {
			continue
		}
		if _, ok := seen[command]; ok {
			continue
		}

		seen[command] = struct{}{}
		unique = append(unique, command)
	}

	sort.Strings(unique)
	return unique
}

func loadConfig() (config, error) {
	var cfg config

	cfg.HTTPAddr = configutil.StringEnv("BEEMINDER_SPOKE_HTTP_ADDR", defaultHTTPAddr)

	cfg.BeeminderBaseURL = strings.TrimRight(configutil.StringEnv("BEEMINDER_API_BASE_URL", defaultBeeminderBaseURL), "/")
	cfg.BeeminderAuthToken = strings.TrimSpace(os.Getenv("BEEMINDER_AUTH_TOKEN"))
	cfg.BeeminderUsername = strings.TrimSpace(os.Getenv("BEEMINDER_USERNAME"))
	cfg.BeeminderGoalSlug = strings.TrimSpace(os.Getenv("BEEMINDER_GOAL_SLUG"))

	cfg.HubNotifyURL = strings.TrimSpace(configutil.StringEnv("DISCORD_HUB_NOTIFY_URL", defaultHubNotifyURL))
	cfg.NotifyTargetChannel = strings.TrimSpace(configutil.StringEnv("BEEMINDER_NOTIFY_CHANNEL", defaultTargetChannel))
	cfg.NotifySeverity = configutil.NormalizeSeverity(configutil.StringEnv("BEEMINDER_NOTIFY_SEVERITY", defaultNotifySeverity))

	var err error

	cfg.PollInterval, err = configutil.DurationEnv("BEEMINDER_POLL_INTERVAL", defaultPollInterval)
	if err != nil {
		return config{}, err
	}

	cfg.ReminderInterval, err = configutil.DurationEnv("BEEMINDER_REMINDER_INTERVAL", defaultReminderInterval)
	if err != nil {
		return config{}, err
	}

	reminderStart := configutil.StringEnv("BEEMINDER_REMINDER_START", defaultReminderStart)
	cfg.ReminderStartHour, cfg.ReminderStartMinute, err = configutil.ParseClockHHMM(reminderStart)
	if err != nil {
		return config{}, fmt.Errorf("invalid BEEMINDER_REMINDER_START: %w", err)
	}

	cfg.ActiveGrace, err = configutil.DurationEnv("BEEMINDER_ACTIVE_GRACE", defaultActiveGrace)
	if err != nil {
		return config{}, err
	}

	cfg.StartedSnooze, err = configutil.DurationEnv("BEEMINDER_STARTED_SNOOZE", defaultStartedSnooze)
	if err != nil {
		return config{}, err
	}

	cfg.DefaultSnooze, err = configutil.DurationEnv("BEEMINDER_DEFAULT_SNOOZE", defaultSnoozeDuration)
	if err != nil {
		return config{}, err
	}

	cfg.HTTPTimeout, err = configutil.DurationEnv("BEEMINDER_HTTP_TIMEOUT", defaultHTTPTimeout)
	if err != nil {
		return config{}, err
	}

	cfg.DatapointsPerPage, err = configutil.IntEnv("BEEMINDER_DATAPOINTS_PER_PAGE", defaultDatapointsPerPage)
	if err != nil {
		return config{}, err
	}

	cfg.MaxDatapointPages, err = configutil.IntEnv("BEEMINDER_MAX_DATAPOINT_PAGES", defaultMaxDatapointPages)
	if err != nil {
		return config{}, err
	}

	cfg.Commands = controlCommands{
		Started: normalizeCommand(configutil.StringEnv("BEEMINDER_COMMAND_STARTED", "started")),
		Snooze:  normalizeCommand(configutil.StringEnv("BEEMINDER_COMMAND_SNOOZE", "snooze")),
		Resume:  normalizeCommand(configutil.StringEnv("BEEMINDER_COMMAND_RESUME", "resume")),
		Status:  normalizeCommand(configutil.StringEnv("BEEMINDER_COMMAND_STATUS", "status")),
	}

	if err := validateConfig(cfg); err != nil {
		return config{}, err
	}

	return cfg, nil
}

func validateConfig(cfg config) error {
	var missing []string

	if cfg.BeeminderAuthToken == "" {
		missing = append(missing, "BEEMINDER_AUTH_TOKEN")
	}
	if cfg.BeeminderUsername == "" {
		missing = append(missing, "BEEMINDER_USERNAME")
	}
	if cfg.BeeminderGoalSlug == "" {
		missing = append(missing, "BEEMINDER_GOAL_SLUG")
	}
	if cfg.HubNotifyURL == "" {
		missing = append(missing, "DISCORD_HUB_NOTIFY_URL")
	}
	if cfg.NotifyTargetChannel == "" {
		missing = append(missing, "BEEMINDER_NOTIFY_CHANNEL")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}

	if cfg.PollInterval <= 0 {
		return errors.New("BEEMINDER_POLL_INTERVAL must be positive")
	}
	if cfg.ReminderInterval <= 0 {
		return errors.New("BEEMINDER_REMINDER_INTERVAL must be positive")
	}
	if cfg.ActiveGrace < 0 {
		return errors.New("BEEMINDER_ACTIVE_GRACE cannot be negative")
	}
	if cfg.StartedSnooze <= 0 {
		return errors.New("BEEMINDER_STARTED_SNOOZE must be positive")
	}
	if cfg.DefaultSnooze <= 0 {
		return errors.New("BEEMINDER_DEFAULT_SNOOZE must be positive")
	}
	if cfg.HTTPTimeout <= 0 {
		return errors.New("BEEMINDER_HTTP_TIMEOUT must be positive")
	}
	if cfg.DatapointsPerPage <= 0 {
		return errors.New("BEEMINDER_DATAPOINTS_PER_PAGE must be positive")
	}
	if cfg.MaxDatapointPages <= 0 {
		return errors.New("BEEMINDER_MAX_DATAPOINT_PAGES must be positive")
	}

	if err := configutil.ValidateSeverity(cfg.NotifySeverity, configutil.DefaultSeverities); err != nil {
		return fmt.Errorf("BEEMINDER_NOTIFY_SEVERITY %w", err)
	}

	allCommands := cfg.Commands.All()
	if len(allCommands) < 4 {
		return errors.New("beeminder command names must be unique and non-empty")
	}

	for _, command := range allCommands {
		if !isValidSlashCommandName(command) {
			return fmt.Errorf("invalid command name %q; use lowercase letters, numbers, _ or -", command)
		}
	}

	return nil
}

func isValidSlashCommandName(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}

	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}

		return false
	}

	return true
}

func normalizeCommand(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
