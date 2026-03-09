package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"personal-infrastructure/pkg/accountability"
	"personal-infrastructure/pkg/spokecontract"
)

type spokeApp struct {
	cfg     config
	service *accountability.Service
}

type commandRequest = spokecontract.CommandRequest
type commandCatalogResponse = spokecontract.CommandCatalog
type commandDefinition = spokecontract.CommandSpec
type commandOptionDefinition = spokecontract.CommandOptionSpec

func (a *spokeApp) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *spokeApp) handleCommands(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, commandCatalogResponse{
		Version: spokecontract.CatalogVersion,
		Service: commandCatalogService,
		Commands: []commandDefinition{
			{Name: a.cfg.CommitCommandName, Description: "Commit to a task with a deadline", Options: []commandOptionDefinition{{Name: "task", Type: "string", Description: "Task description", Required: true}, {Name: "goal", Type: "string", Description: "Beeminder goal slug", Required: true}, {Name: "deadline", Type: "string", Description: "RFC3339 timestamp or duration like 2h", Required: true}}},
			{Name: a.cfg.ProofCommandName, Description: "Submit proof for your active commitment", Options: []commandOptionDefinition{{Name: "proof", Type: "attachment", Description: "Proof attachment", Required: true}}},
			{Name: a.cfg.StatusCommandName, Description: "Show your active commitment"},
			{Name: a.cfg.CancelCommandName, Description: "Cancel your active commitment"},
		},
		Names: []string{a.cfg.CancelCommandName, a.cfg.CommitCommandName, a.cfg.ProofCommandName, a.cfg.StatusCommandName},
	})
}

func (a *spokeApp) handleCommand(w http.ResponseWriter, r *http.Request) {
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	userID := strings.TrimSpace(req.Context.DiscordUserID)
	if userID == "" {
		http.Error(w, "context.discordUserId is required", http.StatusBadRequest)
		return
	}

	cmd := spokecontract.NormalizeCommandName(req.Command)
	now := time.Now().UTC()
	switch cmd {
	case a.cfg.CommitCommandName:
		deadline, err := parseDeadline(now, mapOptionString(req.Options, "deadline"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		commitment, err := a.service.Commit(r.Context(), userID, mapOptionString(req.Options, "task"), mapOptionString(req.Options, "goal"), deadline)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "command": cmd, "message": fmt.Sprintf("Committed until %s for %s", commitment.Deadline.Format(time.RFC3339), commitment.Task), "data": commitment})
	case a.cfg.ProofCommandName:
		attachment := parseAttachment(req.Options["proof"])
		if attachment.ID == "" {
			http.Error(w, "proof attachment is required", http.StatusBadRequest)
			return
		}
		commitment, err := a.service.SubmitProof(r.Context(), userID, attachment)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				http.Error(w, "no active commitment", http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		message := "Proof accepted; commitment completed."
		if commitment.Status == accountability.StatusFailed {
			message = "Commitment missed deadline before proof was submitted."
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "command": cmd, "message": message, "data": commitment})
	case a.cfg.StatusCommandName:
		commitment, err := a.service.StatusForUser(r.Context(), userID)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "command": cmd, "message": "No active commitment."})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "command": cmd, "message": fmt.Sprintf("Active commitment: %s (due %s)", commitment.Task, commitment.Deadline.Format(time.RFC3339)), "data": commitment})
	case a.cfg.CancelCommandName:
		commitment, err := a.service.Cancel(r.Context(), userID)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				http.Error(w, "no active commitment", http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "command": cmd, "message": "Commitment canceled.", "data": commitment})
	default:
		http.Error(w, "unknown command", http.StatusBadRequest)
	}
}

func parseDeadline(now time.Time, raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("deadline is required")
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return now.Add(d), nil
	}
	if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(unix, 0).UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errors.New("deadline must be RFC3339, unix seconds, or duration")
	}
	return parsed.UTC(), nil
}

func parseAttachment(raw any) accountability.AttachmentMetadata {
	if m, ok := raw.(map[string]any); ok {
		return accountability.AttachmentMetadata{ID: mapOptionString(m, "id"), Filename: mapOptionString(m, "filename"), URL: mapOptionString(m, "url"), ContentType: mapOptionString(m, "content_type")}
	}
	if raw == nil {
		return accountability.AttachmentMetadata{}
	}
	return accountability.AttachmentMetadata{ID: strings.TrimSpace(fmt.Sprint(raw))}
}

func mapOptionString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
