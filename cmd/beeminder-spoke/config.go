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
	BeeminderGoalSlugs []string

	HubNotifyURL        string
	HubNotifyAuthToken  string
	NotifyTargetChannel string
	NotifySeverity      string

	PollInterval        time.Duration
	ReminderInterval    time.Duration
	ReminderStartHour   int
	ReminderStartMinute int
	ReminderSchedule    []reminderScheduleStep
	HasBedtime          bool
	BedtimeHour         int
	BedtimeMinute       int

	ActiveGrace      time.Duration
	StartedSnooze    time.Duration
	DefaultSnooze    time.Duration
	MaxSnooze        time.Duration
	RequireDailyRate bool

	HTTPTimeout       time.Duration
	DatapointsPerPage int
	MaxDatapointPages int

	ActionURLs map[string]string

	Commands controlCommands
}

type controlCommands struct {
	Started string
	Snooze  string
	Resume  string
	Status  string
}

type reminderScheduleStep struct {
	StartMinuteOfDay int
	Interval         time.Duration
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
	goalSlugsValue, err := configutil.StringEnvRequired("BEEMINDER_GOAL_SLUGS")
	if err != nil {
		legacySlug := strings.TrimSpace(os.Getenv("BEEMINDER_GOAL_SLUG"))
		if legacySlug == "" {
			return config{}, err
		}
		goalSlugsValue = legacySlug
	}
	cfg.BeeminderGoalSlugs, err = parseGoalSlugs(goalSlugsValue)
	if err != nil {
		return config{}, fmt.Errorf("invalid BEEMINDER_GOAL_SLUGS: %w", err)
	}

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

	reminderScheduleRaw := strings.TrimSpace(os.Getenv("BEEMINDER_REMINDER_SCHEDULE"))
	if reminderScheduleRaw != "" {
		cfg.ReminderSchedule, err = parseReminderSchedule(reminderScheduleRaw)
		if err != nil {
			return config{}, fmt.Errorf("invalid BEEMINDER_REMINDER_SCHEDULE: %w", err)
		}
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

	cfg.MaxSnooze, err = configutil.DurationEnv("BEEMINDER_MAX_SNOOZE", 2*time.Hour)
	if err != nil {
		return config{}, err
	}

	cfg.RequireDailyRate, err = configutil.BoolEnvWithDefaultStrict("BEEMINDER_REQUIRE_DAILY_RATE", true)
	if err != nil {
		return config{}, err
	}

	bedtimeRaw := strings.TrimSpace(os.Getenv("BEEMINDER_BEDTIME"))
	if bedtimeRaw != "" {
		cfg.BedtimeHour, cfg.BedtimeMinute, err = configutil.ParseClockHHMM(bedtimeRaw)
		if err != nil {
			return config{}, fmt.Errorf("invalid BEEMINDER_BEDTIME: %w", err)
		}
		cfg.HasBedtime = true
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

	actionURLsRaw := strings.TrimSpace(os.Getenv("BEEMINDER_ACTION_URLS"))
	cfg.ActionURLs, err = parseActionURLs(actionURLsRaw)
	if err != nil {
		return config{}, fmt.Errorf("invalid BEEMINDER_ACTION_URLS: %w", err)
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
	if len(cfg.BeeminderGoalSlugs) == 0 {
		missing = append(missing, "BEEMINDER_GOAL_SLUGS")
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
	if cfg.MaxSnooze < 0 {
		return errors.New("BEEMINDER_MAX_SNOOZE cannot be negative")
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

	for _, step := range cfg.ReminderSchedule {
		if step.Interval <= 0 {
			return errors.New("BEEMINDER_REMINDER_SCHEDULE intervals must be positive")
		}
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

func parseGoalSlugs(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return nil, errors.New("must provide at least one goal slug")
	}

	seen := make(map[string]struct{}, len(parts))
	slugs := make([]string, 0, len(parts))
	for _, part := range parts {
		slug := strings.TrimSpace(part)
		if slug == "" {
			continue
		}

		if _, exists := seen[slug]; exists {
			continue
		}
		seen[slug] = struct{}{}
		slugs = append(slugs, slug)
	}

	if len(slugs) == 0 {
		return nil, errors.New("must provide at least one non-empty goal slug")
	}

	return slugs, nil
}

func parseReminderSchedule(raw string) ([]reminderScheduleStep, error) {
	parts := strings.Split(raw, ",")
	steps := make([]reminderScheduleStep, 0, len(parts))

	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}

		segments := strings.SplitN(entry, "=", 2)
		if len(segments) != 2 {
			return nil, fmt.Errorf("invalid schedule entry %q; expected HH:MM=duration", entry)
		}

		hour, minute, err := configutil.ParseClockHHMM(strings.TrimSpace(segments[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid schedule time %q: %w", segments[0], err)
		}

		interval, err := time.ParseDuration(strings.TrimSpace(segments[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid schedule duration %q: %w", segments[1], err)
		}

		steps = append(steps, reminderScheduleStep{
			StartMinuteOfDay: hour*60 + minute,
			Interval:         interval,
		})
	}

	sort.Slice(steps, func(i, j int) bool {
		return steps[i].StartMinuteOfDay < steps[j].StartMinuteOfDay
	})

	if len(steps) == 0 {
		return nil, errors.New("must include at least one schedule entry")
	}

	for i := 1; i < len(steps); i++ {
		if steps[i-1].StartMinuteOfDay == steps[i].StartMinuteOfDay {
			return nil, fmt.Errorf("duplicate schedule start time at minute %d", steps[i].StartMinuteOfDay)
		}
	}

	return steps, nil
}

func parseActionURLs(raw string) (map[string]string, error) {
	if raw == "" {
		return map[string]string{}, nil
	}

	result := make(map[string]string)
	entries := strings.Split(raw, ",")
	for _, entry := range entries {
		item := strings.TrimSpace(entry)
		if item == "" {
			continue
		}

		segments := strings.SplitN(item, "=", 2)
		if len(segments) != 2 {
			return nil, fmt.Errorf("invalid action URL entry %q; expected goal=url", item)
		}

		slug := strings.TrimSpace(segments[0])
		if slug == "" {
			return nil, fmt.Errorf("invalid action URL entry %q; goal slug is empty", item)
		}

		urlValue := strings.TrimSpace(segments[1])
		if urlValue == "" {
			return nil, fmt.Errorf("invalid action URL entry %q; URL is empty", item)
		}

		if !strings.HasPrefix(urlValue, "http://") && !strings.HasPrefix(urlValue, "https://") {
			return nil, fmt.Errorf("invalid action URL entry %q; URL must start with http:// or https://", item)
		}

		result[slug] = urlValue
	}

	return result, nil
}

func isValidSlashCommandName(value string) bool {
	return spokecontract.ValidateCommandName(value) == nil
}

func normalizeCommand(raw string) string {
	return spokecontract.NormalizeCommandName(raw)
}
