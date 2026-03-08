package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type notifyPayload struct {
	TargetChannel string `json:"targetChannel"`
	Message       string `json:"message"`
	Severity      string `json:"severity"`
}

type hubHandler struct {
	log             *log.Logger
	session         *discordgo.Session
	channelNameToID map[string]string
	criticalMention string
}

func (h *hubHandler) notify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	maxBodyBytes := int64(1 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()

	var payload notifyPayload
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		h.writeBadRequest(w, fmt.Sprintf("invalid json payload: %v", err))
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeBadRequest(w, "invalid json payload: only one json object is allowed")
		return
	}

	if err := validateNotifyPayload(&payload); err != nil {
		h.writeBadRequest(w, err.Error())
		return
	}

	channelID, ok := h.channelNameToID[payload.TargetChannel]
	if !ok || channelID == "" {
		h.writeBadRequest(w, "unknown targetChannel; configure a channel mapping")
		return
	}

	message := payload.Message
	if payload.Severity == "critical" && h.criticalMention != "" {
		message = h.criticalMention + " " + message
	}

	if _, err := h.session.ChannelMessageSend(channelID, message); err != nil {
		h.log.Printf("failed to send discord message (channel=%s severity=%s): %v", payload.TargetChannel, payload.Severity, err)
		http.Error(w, "failed to dispatch discord message", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(w, `{"status":"sent"}`)
}

func (h *hubHandler) writeBadRequest(w http.ResponseWriter, message string) {
	h.log.Printf("bad request: %s", message)
	http.Error(w, message, http.StatusBadRequest)
}

func validateNotifyPayload(payload *notifyPayload) error {
	payload.TargetChannel = strings.TrimSpace(payload.TargetChannel)
	payload.Message = strings.TrimSpace(payload.Message)
	payload.Severity = strings.ToLower(strings.TrimSpace(payload.Severity))

	if payload.TargetChannel == "" {
		return errors.New("targetChannel is required")
	}
	if payload.Message == "" {
		return errors.New("message is required")
	}
	if _, ok := allowedSeverities[payload.Severity]; !ok {
		return errors.New("severity must be one of: info, warning, critical")
	}

	return nil
}
