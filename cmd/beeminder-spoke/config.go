package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"personal-infrastructure/pkg/configutil"
	"personal-infrastructure/pkg/spokecontract"
)

type config struct {
	HTTPAddr string

	BeeminderBaseURL   string
	BeeminderAuthToken string
	BeeminderUsername  string
	BeeminderGoalSlug  string

	HubNotifyURL        string
	HubNotifyAuthToken  string
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
	var err error

	cfg.HTTPAddr, err = configutil.StringEnvRequired("BEEMINDER_SPOKE_HTTP_ADDR")
	if err != nil {
		return config{}, err
	}

	cfg.BeeminderBaseURL, err = configutil.StringEnvRequired("BEEMINDER_API_BASE_URL")
	if err != nil {
		return config{}, err
	}
	cfg.BeeminderBaseURL = strings.TrimRight(cfg.BeeminderBaseURL, "/")
	cfg.BeeminderAuthToken = strings.TrimSpace(os.Getenv("BEEMINDER_AUTH_TOKEN"))
	cfg.BeeminderUsername = strings.TrimSpace(os.Getenv("BEEMINDER_USERNAME"))
	cfg.BeeminderGoalSlug = strings.TrimSpace(os.Getenv("BEEMINDER_GOAL_SLUG"))

	cfg.HubNotifyURL, err = configutil.StringEnvRequired("DISCORD_HUB_NOTIFY_URL")
	if err != nil {
		return config{}, err
	}
	cfg.HubNotifyURL = strings.TrimSpace(cfg.HubNotifyURL)
	cfg.HubNotifyAuthToken, err = configutil.StringEnvRequired("DISCORD_HUB_NOTIFY_AUTH_TOKEN")
	if err != nil {
		return config{}, err
	}
	cfg.HubNotifyAuthToken = strings.TrimSpace(cfg.HubNotifyAuthToken)
	cfg.NotifyTargetChannel, err = configutil.StringEnvRequired("BEEMINDER_NOTIFY_CHANNEL")
	if err != nil {
		return config{}, err
	}
	cfg.NotifyTargetChannel = strings.TrimSpace(cfg.NotifyTargetChannel)
	notifySeverity, err := configutil.StringEnvRequired("BEEMINDER_NOTIFY_SEVERITY")
	if err != nil {
		return config{}, err
	}
	cfg.NotifySeverity = configutil.NormalizeSeverity(notifySeverity)

	cfg.PollInterval, err = configutil.DurationEnvRequired("BEEMINDER_POLL_INTERVAL")
	if err != nil {
		return config{}, err
	}

	cfg.ReminderInterval, err = configutil.DurationEnvRequired("BEEMINDER_REMINDER_INTERVAL")
	if err != nil {
		return config{}, err
	}

	reminderStart, err := configutil.StringEnvRequired("BEEMINDER_REMINDER_START")
	if err != nil {
		return config{}, err
	}
	cfg.ReminderStartHour, cfg.ReminderStartMinute, err = configutil.ParseClockHHMM(reminderStart)
	if err != nil {
		return config{}, fmt.Errorf("invalid BEEMINDER_REMINDER_START: %w", err)
	}

	cfg.ActiveGrace, err = configutil.DurationEnvRequired("BEEMINDER_ACTIVE_GRACE")
	if err != nil {
		return config{}, err
	}

	cfg.StartedSnooze, err = configutil.DurationEnvRequired("BEEMINDER_STARTED_SNOOZE")
	if err != nil {
		return config{}, err
	}

	cfg.DefaultSnooze, err = configutil.DurationEnvRequired("BEEMINDER_DEFAULT_SNOOZE")
	if err != nil {
		return config{}, err
	}

	cfg.HTTPTimeout, err = configutil.DurationEnvRequired("BEEMINDER_HTTP_TIMEOUT")
	if err != nil {
		return config{}, err
	}

	cfg.DatapointsPerPage, err = configutil.IntEnvRequired("BEEMINDER_DATAPOINTS_PER_PAGE")
	if err != nil {
		return config{}, err
	}

	cfg.MaxDatapointPages, err = configutil.IntEnvRequired("BEEMINDER_MAX_DATAPOINT_PAGES")
	if err != nil {
		return config{}, err
	}

	cfg.Commands = controlCommands{
		Started: normalizeCommand(os.Getenv("BEEMINDER_COMMAND_STARTED")),
		Snooze:  normalizeCommand(os.Getenv("BEEMINDER_COMMAND_SNOOZE")),
		Resume:  normalizeCommand(os.Getenv("BEEMINDER_COMMAND_RESUME")),
		Status:  normalizeCommand(os.Getenv("BEEMINDER_COMMAND_STATUS")),
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
	if cfg.HubNotifyAuthToken == "" {
		missing = append(missing, "DISCORD_HUB_NOTIFY_AUTH_TOKEN")
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
	return spokecontract.ValidateCommandName(value) == nil
}

func normalizeCommand(raw string) string {
	return spokecontract.NormalizeCommandName(raw)
}
