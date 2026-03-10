package main

import (
	"fmt"
	"net/http"
	"time"

	"personal-infrastructure/pkg/httpjson"
	"personal-infrastructure/pkg/spokecontract"
	"personal-infrastructure/pkg/spokecontrol"
)

type commandRequest = spokecontract.CommandRequest
type commandCatalogResponse = spokecontract.CommandCatalog
type commandDefinition = spokecontract.CommandSpec

func (a *financeApp) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	httpjson.Write(w, http.StatusOK, commandCatalogResponse{
		Version: spokecontract.CatalogVersion,
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
	if !spokecontrol.IsAuthorized(r, a.cfg.SpokeCommandAuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req commandRequest
	if err := httpjson.DecodeBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := spokecontrol.ValidateDiscordUser(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	commandName := spokecontrol.NormalizeCommand(req)
	switch commandName {
	case commandNameStatus:
		now := time.Now()
		payload := a.statusPayload(now)
		message := fmt.Sprintf(
			"Finance status: summaryEnabled=%t, nextScheduledAt=%s.",
			a.cfg.SummaryEnabled,
			a.scheduler.nextScheduleAfter(now).In(a.location).Format(time.RFC3339),
		)
		httpjson.Write(w, http.StatusOK, spokecontrol.OK(commandNameStatus, message, payload))
	default:
		http.Error(w, spokecontrol.UnknownCommandError(req.Command, []string{commandNameStatus}), http.StatusBadRequest)
	}
}
