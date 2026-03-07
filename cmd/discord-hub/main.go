package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"

	applog "personal-infrastructure/pkg/logger"
)

const (
	defaultHTTPAddr = "127.0.0.1:8080"
	pingCommandName = "ping"
)

var allowedSeverities = map[string]struct{}{
	"info":     {},
	"warning":  {},
	"critical": {},
}

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

func main() {
	logger := applog.New("discord-hub")

	if err := godotenv.Load(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Printf("unable to load .env file: %v", err)
		}
	}

	token := strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))
	if token == "" {
		logger.Fatal("DISCORD_BOT_TOKEN is required")
	}
	if !strings.HasPrefix(token, "Bot ") {
		token = "Bot " + token
	}

	session, err := discordgo.New(token)
	if err != nil {
		logger.Fatalf("failed to create discord session: %v", err)
	}
	session.Identify.Intents = discordgo.IntentsGuilds
	session.AddHandler(pingInteractionHandler(logger))

	if err := session.Open(); err != nil {
		logger.Fatalf("failed to open discord session: %v", err)
	}
	defer session.Close()

	appID := ""
	if session.State != nil && session.State.User != nil {
		appID = session.State.User.ID
	}
	if appID == "" {
		logger.Fatal("could not resolve discord application id")
	}

	guildID := strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID"))
	if guildID == "" {
		logger.Println("DISCORD_GUILD_ID not set; /ping will register globally and can take up to 1 hour to appear")
	}

	if _, err := upsertPingCommand(session, appID, guildID); err != nil {
		logger.Fatalf("failed to register /ping command: %v", err)
	}

	channelMap := buildChannelMap()
	if len(channelMap) == 0 {
		logger.Println("warning: no channel mappings configured yet; /notify will return HTTP 400")
	}

	handler := &hubHandler{
		log:             logger,
		session:         session,
		channelNameToID: channelMap,
		criticalMention: strings.TrimSpace(os.Getenv("DISCORD_CRITICAL_MENTION")),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/notify", handler.notify)
	mux.HandleFunc("/healthz", healthz)

	addr := strings.TrimSpace(os.Getenv("HUB_HTTP_ADDR"))
	if addr == "" {
		addr = defaultHTTPAddr
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	httpErrCh := make(chan error, 1)
	go func() {
		logger.Printf("HTTP server listening on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- err
			return
		}
		httpErrCh <- nil
	}()

	logger.Println("discord-hub is running")

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-signalCh:
		logger.Printf("received signal: %s", sig)
	case err := <-httpErrCh:
		if err != nil {
			logger.Fatalf("http server failed: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("HTTP shutdown error: %v", err)
	}
	logger.Println("discord-hub stopped")
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`)
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

func buildChannelMap() map[string]string {
	channelMap := map[string]string{
		"kalshi-alerts":    strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_KALSHI_ALERTS")),
		"mandarin-streaks": strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_MANDARIN_STREAKS")),
	}

	extras := strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_MAP"))
	if extras != "" {
		pairs := strings.Split(extras, ",")
		for _, pair := range pairs {
			parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(parts) != 2 {
				continue
			}

			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key == "" || value == "" {
				continue
			}

			channelMap[key] = value
		}
	}

	for key, value := range channelMap {
		if value == "" {
			delete(channelMap, key)
		}
	}

	return channelMap
}

func pingInteractionHandler(logger *log.Logger) func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i == nil || i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		if i.ApplicationCommandData().Name != pingCommandName {
			return
		}

		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "pong",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			logger.Printf("failed to respond to /ping: %v", err)
		}
	}
}

func upsertPingCommand(session *discordgo.Session, appID, guildID string) (*discordgo.ApplicationCommand, error) {
	command := &discordgo.ApplicationCommand{
		Name:        pingCommandName,
		Description: "Check whether discord-hub is alive",
	}

	existingCommands, err := session.ApplicationCommands(appID, guildID)
	if err != nil {
		return nil, fmt.Errorf("could not list existing commands: %w", err)
	}

	for _, existing := range existingCommands {
		if existing.Name == command.Name {
			edited, editErr := session.ApplicationCommandEdit(appID, guildID, existing.ID, command)
			if editErr != nil {
				return nil, fmt.Errorf("could not update existing /ping command: %w", editErr)
			}
			return edited, nil
		}
	}

	created, err := session.ApplicationCommandCreate(appID, guildID, command)
	if err != nil {
		return nil, fmt.Errorf("could not create /ping command: %w", err)
	}

	return created, nil
}
