package main

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"

	"personal-infrastructure/pkg/httpjson"
)

type notifyPayload struct {
	TargetChannel string `json:"targetChannel"`
	Message       string `json:"message"`
	Severity      string `json:"severity"`
}

type hubHandler struct {
	log             *log.Logger
	session         discordMessageSender
	channelNameToID map[string]string
	criticalMention string
}

type discordMessageSender interface {
	ChannelMessageSend(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

func (h *hubHandler) notify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload notifyPayload
	if err := httpjson.DecodeStrictJSONBody(r, &payload, 1<<20); err != nil {
		h.writeBadRequest(w, err.Error())
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

	httpjson.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
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
