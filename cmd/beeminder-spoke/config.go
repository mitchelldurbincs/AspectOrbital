package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	"personal-infrastructure/pkg/configutil"
	"personal-infrastructure/pkg/spokecontract"
)

type config struct {
	HTTPAddr string `envconfig:"BEEMINDER_SPOKE_HTTP_ADDR" required:"true"`

	BeeminderBaseURL   string `envconfig:"BEEMINDER_API_BASE_URL" required:"true"`
	BeeminderAuthToken string `envconfig:"BEEMINDER_AUTH_TOKEN"` // Validated manually to match old logic
	BeeminderUsername  string `envconfig:"BEEMINDER_USERNAME"`

	BeeminderGoalSlugsRaw string   `envconfig:"BEEMINDER_GOAL_SLUGS"`
	BeeminderGoalSlugRaw  string   `envconfig:"BEEMINDER_GOAL_SLUG"`
	BeeminderGoalSlugs    []string `ignored:"true"`

	HubNotifyURL        string `envconfig:"DISCORD_HUB_NOTIFY_URL" required:"true"`
	HubNotifyAuthToken  string `envconfig:"DISCORD_HUB_NOTIFY_AUTH_TOKEN" required:"true"`
	NotifyTargetChannel string `envconfig:"BEEMINDER_NOTIFY_CHANNEL" required:"true"`
	NotifySeverity      string `envconfig:"BEEMINDER_NOTIFY_SEVERITY" required:"true"`

	PollInterval     time.Duration `envconfig:"BEEMINDER_POLL_INTERVAL" required:"true"`
	ReminderInterval time.Duration `envconfig:"BEEMINDER_REMINDER_INTERVAL" required:"true"`

	ReminderStartRaw    string `envconfig:"BEEMINDER_REMINDER_START" required:"true"`
	ReminderStartHour   int    `ignored:"true"`
	ReminderStartMinute int    `ignored:"true"`

	ReminderScheduleRaw string                 `envconfig:"BEEMINDER_REMINDER_SCHEDULE"`
	ReminderSchedule    []reminderScheduleStep `ignored:"true"`

	BedtimeRaw    string `envconfig:"BEEMINDER_BEDTIME"`
	HasBedtime    bool   `ignored:"true"`
	BedtimeHour   int    `ignored:"true"`
	BedtimeMinute int    `ignored:"true"`

	ActiveGrace      time.Duration `envconfig:"BEEMINDER_ACTIVE_GRACE" required:"true"`
	StartedSnooze    time.Duration `envconfig:"BEEMINDER_STARTED_SNOOZE" required:"true"`
	DefaultSnooze    time.Duration `envconfig:"BEEMINDER_DEFAULT_SNOOZE" required:"true"`
	MaxSnooze        time.Duration `envconfig:"BEEMINDER_MAX_SNOOZE" default:"2h"`
	RequireDailyRate bool          `envconfig:"BEEMINDER_REQUIRE_DAILY_RATE" default:"true"`

	HTTPTimeout       time.Duration `envconfig:"BEEMINDER_HTTP_TIMEOUT" required:"true"`
	DatapointsPerPage int           `envconfig:"BEEMINDER_DATAPOINTS_PER_PAGE" required:"true"`
	MaxDatapointPages int           `envconfig:"BEEMINDER_MAX_DATAPOINT_PAGES" required:"true"`

	ActionURLsRaw string            `envconfig:"BEEMINDER_ACTION_URLS"`
	ActionURLs    map[string]string `ignored:"true"`

	CommandStartedRaw string `envconfig:"BEEMINDER_COMMAND_STARTED"`
	CommandSnoozeRaw  string `envconfig:"BEEMINDER_COMMAND_SNOOZE"`
	CommandResumeRaw  string `envconfig:"BEEMINDER_COMMAND_RESUME"`
	CommandStatusRaw  string `envconfig:"BEEMINDER_COMMAND_STATUS"`

	Commands controlCommands `ignored:"true"`
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
	if err := envconfig.Process("", &cfg); err != nil {
		return config{}, err
	}

	goalSlugsValue := cfg.BeeminderGoalSlugsRaw
	if goalSlugsValue == "" {
		goalSlugsValue = cfg.BeeminderGoalSlugRaw
	}
	var err error
	cfg.BeeminderGoalSlugs, err = parseGoalSlugs(goalSlugsValue)
	if err != nil {
		return config{}, fmt.Errorf("invalid BEEMINDER_GOAL_SLUGS: %w", err)
	}

	cfg.NotifySeverity = configutil.NormalizeSeverity(cfg.NotifySeverity)
	if err := configutil.ValidateSeverity(cfg.NotifySeverity, configutil.DefaultSeverities); err != nil {
		return config{}, err
	}

	cfg.ReminderStartHour, cfg.ReminderStartMinute, err = configutil.ParseClockHHMM(cfg.ReminderStartRaw)
	if err != nil {
		return config{}, fmt.Errorf("invalid BEEMINDER_REMINDER_START: %w", err)
	}

	if strings.TrimSpace(cfg.ReminderScheduleRaw) != "" {
		cfg.ReminderSchedule, err = parseReminderSchedule(cfg.ReminderScheduleRaw)
		if err != nil {
			return config{}, fmt.Errorf("invalid BEEMINDER_REMINDER_SCHEDULE: %w", err)
		}
	}

	if strings.TrimSpace(cfg.BedtimeRaw) != "" {
		cfg.BedtimeHour, cfg.BedtimeMinute, err = configutil.ParseClockHHMM(cfg.BedtimeRaw)
		if err != nil {
			return config{}, fmt.Errorf("invalid BEEMINDER_BEDTIME: %w", err)
		}
		cfg.HasBedtime = true
	}

	cfg.ActionURLs, err = parseActionURLs(cfg.ActionURLsRaw)
	if err != nil {
		return config{}, fmt.Errorf("invalid BEEMINDER_ACTION_URLS: %w", err)
	}

	cfg.Commands = controlCommands{
		Started: normalizeCommand(cfg.CommandStartedRaw),
		Snooze:  normalizeCommand(cfg.CommandSnoozeRaw),
		Resume:  normalizeCommand(cfg.CommandResumeRaw),
		Status:  normalizeCommand(cfg.CommandStatusRaw),
	}

	normalizeConfig(&cfg)

	if err := validateConfig(cfg); err != nil {
		return config{}, err
	}

	return cfg, nil
}

func normalizeConfig(cfg *config) {
	if cfg == nil {
		return
	}

	cfg.BeeminderBaseURL = strings.TrimRight(strings.TrimSpace(cfg.BeeminderBaseURL), "/")
}

func validateConfig(cfg config) error {
	var missing []string

	if strings.TrimSpace(cfg.BeeminderAuthToken) == "" {
		missing = append(missing, "BEEMINDER_AUTH_TOKEN")
	}
	if strings.TrimSpace(cfg.BeeminderUsername) == "" {
		missing = append(missing, "BEEMINDER_USERNAME")
	}
	if len(cfg.BeeminderGoalSlugs) == 0 {
		missing = append(missing, "BEEMINDER_GOAL_SLUGS")
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
	partsRaw := strings.Split(raw, ",")
	parts := make([]string, 0, len(partsRaw))
	for _, p := range partsRaw {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) == 0 {
		return nil, errors.New("must provide at least one goal slug")
	}

	seen := make(map[string]struct{}, len(parts))
	slugs := make([]string, 0, len(parts))
	for _, part := range parts {
		slug := part
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
