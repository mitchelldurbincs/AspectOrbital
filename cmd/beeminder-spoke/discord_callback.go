package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"personal-infrastructure/pkg/discordcallback"
	"personal-infrastructure/pkg/hubnotify"
	"personal-infrastructure/pkg/spokecontract"
)

func (a *spokeApp) handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	discordcallback.HandleHTTP(w, r, discordcallback.Options{
		AuthToken:       a.cfg.CallbackAuthToken,
		ExpectedService: commandCatalogService,
		ExpectedEvent:   beeminderNotifyEvent,
	}, a.dispatchDiscordCallback)
}

func (a *spokeApp) dispatchDiscordCallback(r *http.Request, payload hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, int, error) {
	commandPayload, err := callbackActionToCommand(a, payload)
	if err != nil {
		return hubnotify.ActionCallbackResponse{}, http.StatusBadRequest, err
	}

	result, statusCode, err := a.executeCommand(time.Now().UTC(), commandPayload)
	if err != nil {
		return hubnotify.ActionCallbackResponse{}, statusCode, err
	}

	message, _ := result["message"].(string)
	message = strings.TrimSpace(message)
	if message == "" {
		return hubnotify.ActionCallbackResponse{}, http.StatusInternalServerError, errors.New("callback command returned empty message")
	}

	return hubnotify.ActionCallbackResponse{Status: "ok", Message: message}, http.StatusOK, nil
}

func callbackActionToCommand(a *spokeApp, payload hubnotify.ActionCallbackRequest) (commandRequest, error) {
	ctx := spokecontract.CommandContext{
		DiscordUserID: strings.TrimSpace(payload.Context.DiscordUserID),
		GuildID:       strings.TrimSpace(payload.Context.GuildID),
		ChannelID:     strings.TrimSpace(payload.Context.ChannelID),
	}

	switch strings.TrimSpace(payload.Action.ID) {
	case discordActionSnooze10m:
		return commandRequest{
			Command: a.cfg.Commands.Snooze,
			Context: ctx,
			Options: map[string]any{snoozeDurationOptionName: "10m"},
		}, nil
	case discordActionSnooze30m:
		return commandRequest{
			Command: a.cfg.Commands.Snooze,
			Context: ctx,
			Options: map[string]any{snoozeDurationOptionName: "30m"},
		}, nil
	case discordActionAcknowledge:
		return commandRequest{
			Command: a.cfg.Commands.Started,
			Context: ctx,
		}, nil
	default:
		return commandRequest{}, errors.New("unknown action id")
	}
}
