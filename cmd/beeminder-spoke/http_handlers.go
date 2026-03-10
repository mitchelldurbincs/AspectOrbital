package main

import (
	"net/http"
	"time"

	"personal-infrastructure/pkg/httpjson"
	"personal-infrastructure/pkg/spokecontrol"
)

func (a *spokeApp) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *spokeApp) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	httpjson.Write(w, http.StatusOK, a.engine.Status())
}

func (a *spokeApp) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	httpjson.Write(w, http.StatusOK, commandCatalogForConfig(a.cfg))
}

func (a *spokeApp) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !spokecontrol.IsAuthorized(r, a.cfg.SpokeCommandAuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload commandRequest
	if err := httpjson.DecodeBody(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := spokecontrol.ValidateDiscordUser(spokecontrol.Request(payload)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, statusCode, err := a.executeCommand(time.Now().UTC(), payload)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	httpjson.Write(w, http.StatusOK, result)
}

func (a *spokeApp) handleStarted(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, _, err := a.executeCommand(time.Now().UTC(), commandRequest{Command: a.cfg.Commands.Started})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	httpjson.Write(w, http.StatusOK, result)
}

func (a *spokeApp) handleSnooze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload snoozeRequest
	if err := httpjson.DecodeBody(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	argument := payload.Duration
	if argument == "" && payload.Minutes > 0 {
		argument = (time.Duration(payload.Minutes) * time.Minute).String()
	}

	result, statusCode, err := a.executeCommand(time.Now().UTC(), commandRequest{
		Command: a.cfg.Commands.Snooze,
		Options: map[string]any{snoozeDurationOptionName: argument},
	})
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	httpjson.Write(w, http.StatusOK, result)
}

func (a *spokeApp) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, _, err := a.executeCommand(time.Now().UTC(), commandRequest{Command: a.cfg.Commands.Resume})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	httpjson.Write(w, http.StatusOK, result)
}

type snoozeRequest struct {
	Duration string `json:"duration"`
	Minutes  int    `json:"minutes"`
}
