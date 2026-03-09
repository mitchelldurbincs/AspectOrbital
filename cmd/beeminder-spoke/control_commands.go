package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"personal-infrastructure/pkg/spokecontract"
)

func (a *spokeApp) executeCommand(now time.Time, request commandRequest) (map[string]any, int, error) {
	command := request.normalizedCommand()
	if command == "" {
		return nil, http.StatusBadRequest, errors.New("command is required")
	}

	switch command {
	case a.cfg.Commands.Started:
		until := a.engine.MarkStarted(now)
		return map[string]any{
			"status":       "ok",
			"command":      a.cfg.Commands.Started,
			"message":      fmt.Sprintf("Got it. Paused reminders until %s.", formatClockInLocation(until, a.location)),
			"snoozedUntil": until,
		}, http.StatusOK, nil
	case a.cfg.Commands.Snooze:
		durationInput := request.optionString(snoozeDurationOptionName)

		duration, err := parseSnoozeArgument(durationInput, a.cfg.DefaultSnooze)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}

		until := a.engine.Snooze(now, duration)
		return map[string]any{
			"status":       "ok",
			"command":      a.cfg.Commands.Snooze,
			"message":      fmt.Sprintf("Snoozed reminders for %s (until %s).", duration, formatClockInLocation(until, a.location)),
			"duration":     duration.String(),
			"snoozedUntil": until,
		}, http.StatusOK, nil
	case a.cfg.Commands.Resume:
		a.engine.Resume(now)
		return map[string]any{
			"status":  "ok",
			"command": a.cfg.Commands.Resume,
			"message": "Reminders resumed.",
		}, http.StatusOK, nil
	case a.cfg.Commands.Status:
		status := a.engine.Status()
		return map[string]any{
			"status":  "ok",
			"command": a.cfg.Commands.Status,
			"message": summarizeStatus(status, a.location),
			"data":    status,
		}, http.StatusOK, nil
	default:
		return nil, http.StatusBadRequest, fmt.Errorf("unknown command %q; valid commands: %s", request.Command, strings.Join(a.cfg.Commands.All(), ", "))
	}
}

type commandRequest struct {
	Command string                       `json:"command"`
	Context spokecontract.CommandContext `json:"context"`
	Options map[string]any               `json:"options,omitempty"`
}

func (c commandRequest) normalizedCommand() string {
	return spokecontract.NormalizeCommandName(c.Command)
}

func (c commandRequest) optionString(name string) string {
	if c.Options == nil {
		return ""
	}

	raw, ok := c.Options[name]
	if !ok {
		return ""
	}

	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return strings.TrimSpace(value.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 64))
	case bool:
		return strings.TrimSpace(strconv.FormatBool(value))
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func parseSnoozeArgument(raw string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q; try values like 15m or 1h", value)
	}
	if duration <= 0 {
		return 0, errors.New("snooze duration must be positive")
	}

	return duration, nil
}
