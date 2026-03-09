package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type commandRequest struct {
	Command  string         `json:"command"`
	Argument string         `json:"argument,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type commandCatalogResponse struct {
	Version  int                 `json:"version"`
	Service  string              `json:"service"`
	Commands []commandDefinition `json:"commands"`
	Names    []string            `json:"commandNames"`
}

type commandDefinition struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Options     []commandOptionDefinition `json:"options,omitempty"`
}

type commandOptionDefinition struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func (a *financeApp) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, commandCatalogResponse{
		Version: commandCatalogVersion,
		Service: commandCatalogService,
		Commands: []commandDefinition{
			{
				Name:        commandNameStatus,
				Description: "Show finance-spoke scheduler and summary state",
			},
		},
		Names: []string{commandNameStatus},
	})
}

func (a *financeApp) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req commandRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	commandName := normalizeCommand(req.Command)
	switch commandName {
	case commandNameStatus:
		now := time.Now()
		payload := a.statusPayload(now)
		message := fmt.Sprintf(
			"Finance status: summaryEnabled=%t, nextScheduledAt=%s.",
			a.cfg.SummaryEnabled,
			a.scheduler.nextScheduleAfter(now).In(a.location).Format(time.RFC3339),
		)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"command": commandNameStatus,
			"message": message,
			"data":    payload,
		})
	default:
		http.Error(w, unknownCommandError(req.Command, []string{commandNameStatus}), http.StatusBadRequest)
	}
}

func normalizeCommand(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func unknownCommandError(requested string, valid []string) string {
	commands := append([]string(nil), valid...)
	sort.Strings(commands)
	return fmt.Sprintf("unknown command %q; valid commands: %s", requested, strings.Join(commands, ", "))
}
