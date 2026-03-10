package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"personal-infrastructure/pkg/appboot"
	"personal-infrastructure/pkg/hubnotify"
	"personal-infrastructure/pkg/lifecycle"
	applog "personal-infrastructure/pkg/logger"
)

const (
	cycleTimeout               = 30 * time.Second
	commandCatalogService      = "beeminder-spoke"
	snoozeDurationOptionName   = "duration"
	snoozeDurationOptionType   = "string"
	snoozeDurationOptionPrompt = "Examples: 15m, 1h"
	discordActionSnooze10m     = "snooze_10m"
	discordActionSnooze30m     = "snooze_30m"
	discordActionAcknowledge   = "acknowledge"
)

func main() {
	logger := applog.New("beeminder-spoke")
	if err := run(logger); err != nil {
		logger.Printf("beeminder-spoke exiting: %v", err)
	}
}

func run(logger *log.Logger) error {
	appboot.LoadEnvFiles(logger, appboot.StandardEnvFiles("cmd/beeminder-spoke")...)

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	beeminder := newBeeminderClient(cfg, httpClient)
	hub := hubnotify.NewClient(cfg.HubNotifyURL, cfg.HubNotifyAuthToken, httpClient)
	engine := newReminderEngine(cfg)

	bootCtx, bootCancel := context.WithTimeout(context.Background(), cycleTimeout)
	defer bootCancel()

	user, err := beeminder.GetUser(bootCtx)
	if err != nil {
		return fmt.Errorf("failed to load beeminder user profile: %w", err)
	}

	location, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return fmt.Errorf("failed to load beeminder timezone %q: %w", user.Timezone, err)
	}

	app := &spokeApp{
		cfg:       cfg,
		log:       logger,
		beeminder: beeminder,
		hub:       hub,
		engine:    engine,
		location:  location,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", app.handleHealthz)
	mux.HandleFunc("/status", app.handleStatus)
	mux.HandleFunc("/control/commands", app.handleCommands)
	mux.HandleFunc("/control/command", app.handleCommand)
	mux.HandleFunc("/control/started", app.handleStarted)
	mux.HandleFunc("/control/snooze", app.handleSnooze)
	mux.HandleFunc("/control/resume", app.handleResume)
	mux.HandleFunc("/discord/callback", app.handleDiscordCallback)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Printf("tracking Beeminder goals %v for user %q", cfg.BeeminderGoalSlugs, cfg.BeeminderUsername)

	exitErr := lifecycle.RunHTTPService(lifecycle.HTTPServiceOptions{
		Logger:          logger,
		Server:          httpServer,
		ListenMessage:   fmt.Sprintf("control API listening on %s", cfg.HTTPAddr),
		TickInterval:    cfg.PollInterval,
		RunImmediately:  true,
		ShutdownTimeout: 10 * time.Second,
		OnTick: func(context.Context) error {
			if err := app.runCycle(context.Background()); err != nil {
				logger.Printf("beeminder check failed: %v", err)
			}
			return nil
		},
	})

	logger.Println("beeminder-spoke stopped")
	return exitErr
}
