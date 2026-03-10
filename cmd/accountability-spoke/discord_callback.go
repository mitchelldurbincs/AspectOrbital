package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"personal-infrastructure/pkg/accountability"
	"personal-infrastructure/pkg/discordcallback"
	"personal-infrastructure/pkg/hubnotify"
)

func (a *spokeApp) handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	discordcallback.HandleHTTP(w, r, discordcallback.Options{
		AuthToken:       a.cfg.CallbackAuthToken,
		ExpectedService: commandCatalogService,
		ExpectedEvent:   accountabilityNotifyEvent,
	}, a.dispatchDiscordCallback)
}

func (a *spokeApp) dispatchDiscordCallback(r *http.Request, payload hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, int, error) {
	userID := strings.TrimSpace(payload.Context.DiscordUserID)

	switch strings.TrimSpace(payload.Action.ID) {
	case accountabilityActionSnooze30m:
		commitment, err := a.service.Snooze(r.Context(), userID, 30*time.Minute, a.cfg.MaxSnooze)
		if err != nil {
			return hubnotify.ActionCallbackResponse{}, accountabilityHTTPStatus(err), err
		}
		return hubnotify.ActionCallbackResponse{Status: "ok", Message: fmt.Sprintf("Reminders snoozed until %s", commitment.SnoozedUntil.Format(time.RFC3339))}, http.StatusOK, nil
	case accountabilityActionDismiss:
		commitment, err := a.service.Snooze(r.Context(), userID, a.cfg.DefaultSnooze, a.cfg.MaxSnooze)
		if err != nil {
			return hubnotify.ActionCallbackResponse{}, accountabilityHTTPStatus(err), err
		}
		return hubnotify.ActionCallbackResponse{Status: "ok", Message: fmt.Sprintf("Dismissed this reminder until %s", commitment.SnoozedUntil.Format(time.RFC3339))}, http.StatusOK, nil
	default:
		return hubnotify.ActionCallbackResponse{}, http.StatusBadRequest, errors.New("unknown action id")
	}
}

func accountabilityHTTPStatus(err error) int {
	switch {
	case errors.Is(err, accountability.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, accountability.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, accountability.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
