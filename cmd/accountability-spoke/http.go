package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"personal-infrastructure/pkg/accountability"
	"personal-infrastructure/pkg/spokecontract"
	"personal-infrastructure/pkg/spokecontrol"
)

type spokeApp struct {
	cfg      config
	service  *accountability.Service
	policies policyCatalog
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
			{Name: a.cfg.CommitCommandName, Description: "Commit to a task with a deadline", Options: []commandOptionDefinition{{Name: "deadline", Type: "string", Description: "RFC3339 timestamp or duration like 2h", Required: true}, {Name: "task", Type: "string", Description: "Task description override", Required: false}, {Name: "preset", Type: "string", Description: "Policy preset name override", Required: false}}},
			{Name: a.cfg.ProofCommandName, Description: "Submit proof for your active commitment", Options: []commandOptionDefinition{{Name: "proof", Type: "attachment", Description: "Proof attachment", Required: false}, {Name: "text", Type: "string", Description: "Proof text reply", Required: false}}},
			{Name: a.cfg.SnoozeCommandName, Description: "Snooze reminders for your active commitment", Options: []commandOptionDefinition{{Name: "duration", Type: "string", Description: "Duration like 10m", Required: false}}},
			{Name: a.cfg.StatusCommandName, Description: "Show your active commitment"},
			{Name: a.cfg.CancelCommandName, Description: "Cancel your active commitment"},
		},
		Names: []string{a.cfg.CancelCommandName, a.cfg.CommitCommandName, a.cfg.ProofCommandName, a.cfg.SnoozeCommandName, a.cfg.StatusCommandName},
	})
}

func (a *spokeApp) handleCommand(w http.ResponseWriter, r *http.Request) {
	var req commandRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	userID := strings.TrimSpace(req.Context.DiscordUserID)
	if err := spokecontrol.ValidateDiscordUser(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cmd := spokecontrol.NormalizeCommand(req)
	now := time.Now()
	switch cmd {
	case a.cfg.CommitCommandName:
		deadline, err := parseDeadline(now, mapOptionString(req.Options, "deadline"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resolvedPolicy, err := a.policies.ResolveCommit(mapOptionString(req.Options, "task"), mapOptionString(req.Options, "preset"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		commitment, err := a.service.CommitWithPolicy(r.Context(), userID, resolvedPolicy.Task, deadline, resolvedPolicy.Preset, resolvedPolicy.Engine, resolvedPolicy.ConfigJSON)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, spokecontrol.OK(cmd, fmt.Sprintf("Committed until %s for %s using preset %s", commitment.Deadline.Format(time.RFC3339), commitment.Task, commitment.PolicyPreset), commitment))
	case a.cfg.ProofCommandName:
		active, err := a.service.StatusForUser(r.Context(), userID)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				http.Error(w, "no active commitment", http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		attachment := parseAttachment(req.Options["proof"])
		if normalizePolicyEngine(active.PolicyEngine) == policyEngineOpenAIVision && strings.TrimSpace(attachment.URL) != "" {
			if _, err := validatePublicImageURL(attachment.URL); err != nil {
				http.Error(w, fmt.Sprintf("invalid proof attachment URL: %v", err), http.StatusBadRequest)
				return
			}
		}
		proofText := mapOptionString(req.Options, "text")
		evaluation, err := a.policies.Evaluate(r.Context(), active, attachment, proofText)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !evaluation.Pass {
			http.Error(w, evaluation.Reason, http.StatusBadRequest)
			return
		}
		commitment, err := a.service.SubmitProof(r.Context(), userID, accountability.ProofSubmission{Attachment: attachment, Text: proofText, Verdict: evaluation.Verdict})
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
		writeJSON(w, http.StatusOK, spokecontrol.OK(cmd, message, commitment))
	case a.cfg.StatusCommandName:
		commitment, err := a.service.StatusForUser(r.Context(), userID)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				writeJSON(w, http.StatusOK, spokecontrol.OK(cmd, "No active commitment.", nil))
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		message := fmt.Sprintf("Active commitment: %s (due %s)", commitment.Task, commitment.Deadline.Format(time.RFC3339))
		if commitment.PolicyPreset != "" {
			message = fmt.Sprintf("%s; preset=%s", message, commitment.PolicyPreset)
		}
		if !commitment.SnoozedUntil.IsZero() && commitment.SnoozedUntil.After(now) {
			message = fmt.Sprintf("%s; reminders snoozed until %s", message, commitment.SnoozedUntil.Format(time.RFC3339))
		}
		writeJSON(w, http.StatusOK, spokecontrol.OK(cmd, message, commitment))
	case a.cfg.SnoozeCommandName:
		duration, err := parseSnoozeDuration(mapOptionString(req.Options, "duration"), a.cfg.DefaultSnooze)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		commitment, err := a.service.Snooze(r.Context(), userID, duration, a.cfg.MaxSnooze)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				http.Error(w, "no active commitment", http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, spokecontrol.OK(cmd, fmt.Sprintf("Reminders snoozed until %s", commitment.SnoozedUntil.Format(time.RFC3339)), commitment))
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
		writeJSON(w, http.StatusOK, spokecontrol.OK(cmd, "Commitment canceled.", commitment))
	default:
		http.Error(w, spokecontrol.UnknownCommandError(req.Command, []string{a.cfg.CancelCommandName, a.cfg.CommitCommandName, a.cfg.ProofCommandName, a.cfg.SnoozeCommandName, a.cfg.StatusCommandName}), http.StatusBadRequest)
	}
}

func parseDeadline(now time.Time, raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("deadline is required")
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return now.Add(d).UTC(), nil
	}
	if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(unix, 0).UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return parsed.UTC(), nil
	}
	clockLayouts := []string{"3:04pm", "3:04 pm", "3pm", "3 pm", "15:04"}
	for _, layout := range clockLayouts {
		clock, clockErr := time.ParseInLocation(layout, strings.ToLower(raw), now.Location())
		if clockErr != nil {
			continue
		}
		candidate := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
		if !candidate.After(now) {
			candidate = candidate.Add(24 * time.Hour)
		}
		return candidate.UTC(), nil
	}
	return time.Time{}, errors.New("deadline must be RFC3339, unix seconds, duration, or a clock time like 4:30am")
}

func parseAttachment(raw any) accountability.AttachmentMetadata {
	if m, ok := raw.(map[string]any); ok {
		contentType := mapOptionString(m, "content_type")
		if contentType == "" {
			contentType = mapOptionString(m, "contentType")
		}
		size := 0
		if rawSize, ok := m["size"]; ok {
			if parsedSize, err := intFromAny(rawSize); err == nil && parsedSize >= 0 {
				size = parsedSize
			}
		}
		return accountability.AttachmentMetadata{ID: mapOptionString(m, "id"), Filename: mapOptionString(m, "filename"), URL: mapOptionString(m, "url"), ContentType: contentType, Size: size}
	}
	if raw == nil {
		return accountability.AttachmentMetadata{}
	}
	return accountability.AttachmentMetadata{ID: strings.TrimSpace(fmt.Sprint(raw))}
}

func parseSnoozeDuration(raw string, defaultDuration time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultDuration, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, errors.New("duration must be a valid duration like 10m")
	}
	if d <= 0 {
		return 0, errors.New("duration must be positive")
	}
	return d, nil
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
