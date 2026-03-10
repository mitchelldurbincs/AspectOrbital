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
	"personal-infrastructure/pkg/spokecontrol"
)

func (a *spokeApp) executeCommand(now time.Time, request commandRequest) (map[string]any, int, error) {
	command := request.Command
	if command == "" {
		return nil, http.StatusBadRequest, errors.New("command is required")
	}

	switch command {
	case a.cfg.Commands.Started:
		until := a.engine.MarkStarted(now)
		result := spokecontrol.OK(a.cfg.Commands.Started, fmt.Sprintf("Got it. Paused reminders until %s.", formatClockInLocation(until, a.location)), nil)
		result["snoozedUntil"] = until
		return result, http.StatusOK, nil
	case a.cfg.Commands.Snooze:
		durationInput := request.optionString(snoozeDurationOptionName)

		duration, err := parseSnoozeArgument(durationInput, a.cfg.MaxSnooze)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}

		until := a.engine.Snooze(now, duration)
		message := fmt.Sprintf("Snoozed reminders for %s (until %s).", duration, formatClockInLocation(until, a.location))

		result := spokecontrol.OK(a.cfg.Commands.Snooze, message, nil)
		result["duration"] = duration.String()
		result["snoozedUntil"] = until
		return result, http.StatusOK, nil
	case a.cfg.Commands.Resume:
		a.engine.Resume(now)
		return spokecontrol.OK(a.cfg.Commands.Resume, "Reminders resumed.", nil), http.StatusOK, nil
	case a.cfg.Commands.Status:
		status := a.engine.Status()
		return spokecontrol.OK(a.cfg.Commands.Status, summarizeStatus(status, a.location), status), http.StatusOK, nil
	default:
		return nil, http.StatusBadRequest, fmt.Errorf("%s", spokecontrol.UnknownCommandError(request.Command, a.cfg.Commands.All()))
	}
}

type commandRequest struct {
	Command string                       `json:"command"`
	Context spokecontract.CommandContext `json:"context"`
	Options map[string]any               `json:"options,omitempty"`
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

func parseSnoozeArgument(raw string, max time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(raw)

	if value == "" {
		return 0, errors.New("snooze duration is required")
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q; try values like 15m or 1h", value)
	}
	if duration <= 0 {
		return 0, errors.New("snooze duration must be positive")
	}

	if max > 0 && duration > max {
		return 0, fmt.Errorf("snooze duration cannot exceed %s", max)
	}

	return duration, nil
}
