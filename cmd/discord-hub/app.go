package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
)

func runHub(logger *log.Logger, cfg hubConfig) error {
	session, err := discordgo.New(cfg.DiscordToken)
	if err != nil {
		return fmt.Errorf("failed to create discord session: %w", err)
	}
	session.Identify.Intents = discordgo.IntentsGuilds

	spokeBridge := spokebridge.Discover(logger)
	session.AddHandler(interactionHandler(logger, spokeBridge))

	if err := session.Open(); err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}
	defer session.Close()

	appID, err := resolveApplicationID(session)
	if err != nil {
		return err
	}

	if cfg.GuildID == "" {
		logger.Println("DISCORD_GUILD_ID not set; /ping will register globally and can take up to 1 hour to appear")
	}

	if _, err := upsertPingCommand(session, appID, cfg.GuildID); err != nil {
		return fmt.Errorf("failed to register /ping command: %w", err)
	}

	if spokeBridge != nil {
		if err := upsertSpokeCommands(session, appID, cfg.GuildID, spokeBridge); err != nil {
			logger.Printf("warning: failed to register spoke commands: %v", err)
		}
	}

	if len(cfg.ChannelMap) == 0 {
		logger.Println("warning: no channel mappings configured yet; /notify will return HTTP 400")
	}

	handler := &hubHandler{
		log:             logger,
		session:         session,
		channelNameToID: cfg.ChannelMap,
		criticalMention: cfg.CriticalMention,
		notifyAuthToken: cfg.NotifyAuthToken,
	}

	httpServer := newHTTPServer(cfg.HTTPAddr, handler)
	httpErrCh := make(chan error, 1)

	go func() {
		logger.Printf("HTTP server listening on %s", cfg.HTTPAddr)
		if serveErr := httpServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			httpErrCh <- serveErr
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
	case httpErr := <-httpErrCh:
		if httpErr != nil {
			return fmt.Errorf("http server failed: %w", httpErr)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("HTTP shutdown error: %v", err)
	}

	logger.Println("discord-hub stopped")
	return nil
}

func resolveApplicationID(session *discordgo.Session) (string, error) {
	if session.State != nil && session.State.User != nil && session.State.User.ID != "" {
		return session.State.User.ID, nil
	}

	return "", errors.New("could not resolve discord application id")
}

func newHTTPServer(addr string, handler *hubHandler) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/notify", handler.notify)
	mux.HandleFunc("/healthz", healthz)

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
