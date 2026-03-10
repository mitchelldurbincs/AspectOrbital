package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"personal-infrastructure/pkg/discordcallback"
	"personal-infrastructure/pkg/hubnotify"
)

func (a *spokeApp) handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	discordcallback.HandleHTTP(w, r, discordcallback.Options{
		AuthToken:       a.cfg.CallbackAuthToken,
		ExpectedService: commandCatalogService,
		ExpectedEvent:   beeminderNotifyEvent,
	}, a.dispatchDiscordCallback)
}

func (a *spokeApp) dispatchDiscordCallback(r *http.Request, payload hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, int, error) {
	actionID, goalSlug, err := parseScopedDiscordAction(payload.Action.ID)
	if err != nil {
		return hubnotify.ActionCallbackResponse{}, http.StatusBadRequest, err
	}
	if !a.isConfiguredGoal(goalSlug) {
		return hubnotify.ActionCallbackResponse{}, http.StatusBadRequest, fmt.Errorf("unknown goal slug %q", goalSlug)
	}

	now := time.Now().UTC()
	message, statusCode, err := a.executeGoalScopedAction(now, actionID, goalSlug)
	if err != nil {
		return hubnotify.ActionCallbackResponse{}, statusCode, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return hubnotify.ActionCallbackResponse{}, http.StatusInternalServerError, errors.New("callback action returned empty message")
	}

	return hubnotify.ActionCallbackResponse{Status: "ok", Message: message}, http.StatusOK, nil
}

func (a *spokeApp) executeGoalScopedAction(now time.Time, actionID string, goalSlug string) (string, int, error) {
	switch actionID {
	case discordActionSnooze10m:
		until := a.engine.SnoozeGoal(goalSlug, now, 10*time.Minute)
		return fmt.Sprintf("Snoozed reminders for %s for 10m (until %s).", goalSlug, formatClockInLocation(until, a.location)), http.StatusOK, nil
	case discordActionSnooze30m:
		until := a.engine.SnoozeGoal(goalSlug, now, 30*time.Minute)
		return fmt.Sprintf("Snoozed reminders for %s for 30m (until %s).", goalSlug, formatClockInLocation(until, a.location)), http.StatusOK, nil
	case discordActionAcknowledge:
		until := a.engine.MarkStartedGoal(goalSlug, now)
		return fmt.Sprintf("Got it. Paused reminders for %s until %s.", goalSlug, formatClockInLocation(until, a.location)), http.StatusOK, nil
	default:
		return "", http.StatusBadRequest, errors.New("unknown action id")
	}
}

func parseScopedDiscordAction(raw string) (string, string, error) {
	value := strings.TrimSpace(raw)
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "", "", errors.New("action.id must include a goal slug")
	}

	actionID := strings.TrimSpace(parts[0])
	goalSlug := strings.TrimSpace(parts[1])
	if actionID == "" || goalSlug == "" {
		return "", "", errors.New("action.id must include a non-empty action and goal slug")
	}

	return actionID, goalSlug, nil
}

func (a *spokeApp) isConfiguredGoal(goalSlug string) bool {
	for _, configuredGoalSlug := range a.cfg.BeeminderGoalSlugs {
		if strings.TrimSpace(configuredGoalSlug) == strings.TrimSpace(goalSlug) {
			return true
		}
	}

	return false
}
