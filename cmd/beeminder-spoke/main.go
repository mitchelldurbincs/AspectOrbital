package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"personal-infrastructure/pkg/hubnotify"
	applog "personal-infrastructure/pkg/logger"
)

const (
	cycleTimeout               = 30 * time.Second
	commandCatalogVersion      = 1
	commandCatalogService      = "beeminder-spoke"
	snoozeDurationOptionName   = "duration"
	snoozeDurationOptionType   = "string"
	snoozeDurationOptionPrompt = "Examples: 15m, 1h"
)

func main() {
	logger := applog.New("beeminder-spoke")

	for _, envFile := range []string{"cmd/beeminder-spoke/.env", ".env"} {
		if err := godotenv.Load(envFile); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				logger.Printf("unable to load %s: %v", envFile, err)
			}
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		logger.Fatalf("invalid configuration: %v", err)
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	beeminder := newBeeminderClient(cfg, httpClient)
	hub := hubnotify.NewClient(cfg.HubNotifyURL, httpClient)
	engine := newReminderEngine(cfg)

	bootCtx, bootCancel := context.WithTimeout(context.Background(), cycleTimeout)
	defer bootCancel()

	user, err := beeminder.GetUser(bootCtx)
	if err != nil {
		logger.Fatalf("failed to load beeminder user profile: %v", err)
	}

	location, err := time.LoadLocation(user.Timezone)
	if err != nil {
		logger.Fatalf("failed to load beeminder timezone %q: %v", user.Timezone, err)
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

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	httpErrCh := make(chan error, 1)
	go func() {
		logger.Printf("control API listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- err
			return
		}
		httpErrCh <- nil
	}()

	if err := app.runCycle(context.Background()); err != nil {
		logger.Printf("initial beeminder check failed: %v", err)
	}

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

	logger.Printf("tracking Beeminder goal %q for user %q", cfg.BeeminderGoalSlug, cfg.BeeminderUsername)

	running := true
	for running {
		select {
		case <-ticker.C:
			if err := app.runCycle(context.Background()); err != nil {
				logger.Printf("beeminder check failed: %v", err)
			}
		case sig := <-signalCh:
			logger.Printf("received signal: %s", sig)
			running = false
		case err := <-httpErrCh:
			if err != nil {
				logger.Fatalf("control API failed: %v", err)
			}
			running = false
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("control API shutdown error: %v", err)
	}

	logger.Println("beeminder-spoke stopped")
}
