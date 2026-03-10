package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"personal-infrastructure/pkg/accountability"
	"personal-infrastructure/pkg/appboot"
	"personal-infrastructure/pkg/hubnotify"
	"personal-infrastructure/pkg/lifecycle"
)

const (
	commandCatalogService         = "accountability-spoke"
	accountabilityNotifyEvent     = "commitment-reminder"
	accountabilityActionSnooze30m = "snooze_30m"
	accountabilityActionDismiss   = "dismiss"
)

func main() {
	logger := log.New(os.Stdout, "accountability-spoke ", log.LstdFlags|log.Lmicroseconds)
	if err := run(logger); err != nil {
		logger.Printf("accountability-spoke exiting: %v", err)
	}
}

func run(logger *log.Logger) error {
	appboot.LoadEnvFiles(logger, appboot.StandardEnvFiles("cmd/accountability-spoke")...)

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	warnOnKnownSpokePortCollisions(logger)

	db, err := accountability.OpenDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer closeDB(logger, db)

	if err := accountability.Bootstrap(context.Background(), db); err != nil {
		return fmt.Errorf("bootstrap db: %w", err)
	}

	service, err := accountability.NewService(db, cfg.ExpiryPollInterval, cfg.ExpiryGracePeriod)
	if err != nil {
		return fmt.Errorf("init accountability service: %w", err)
	}
	hub := hubnotify.NewClient(cfg.HubNotifyURL, cfg.HubNotifyAuthToken, &http.Client{Timeout: 10 * time.Second})
	openAIClient := newOpenAIVisionClient(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel, &http.Client{Timeout: cfg.OpenAITimeout})
	policies, err := loadPolicyCatalog(cfg.PolicyFile, openAIClient)
	if err != nil {
		return fmt.Errorf("invalid policy configuration: %w", err)
	}

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()
	go service.StartExpiryLoop(loopCtx)
	go startReminderLoop(loopCtx, logger, cfg, service, hub)
	if _, err := service.ExpireOverdue(loopCtx); err != nil {
		logger.Printf("initial expiry sweep failed: %v", err)
	}
	if err := runReminderSweep(loopCtx, logger, cfg, service, hub); err != nil {
		logger.Printf("initial reminder sweep failed: %v", err)
	}

	app := &spokeApp{cfg: cfg, service: service, policies: policies}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", app.handleHealthz)
	mux.HandleFunc("/control/commands", app.handleCommands)
	mux.HandleFunc("/control/command", app.handleCommand)
	mux.HandleFunc("/discord/callback", app.handleDiscordCallback)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	exitErr := lifecycle.WaitForExit(sigCh, errCh, nil, func() {})

	loopCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf("control API shutdown error: %v", err)
	}
	logger.Println("accountability-spoke stopped")
	if exitErr != nil {
		logger.Printf("exit error: %v", exitErr)
		os.Exit(1)
	}
	return nil
}

func closeDB(logger *log.Logger, db *sql.DB) {
	if db == nil {
		return
	}
	if err := db.Close(); err != nil {
		logger.Printf("db close error: %v", err)
	}
}

func startReminderLoop(ctx context.Context, logger *log.Logger, cfg config, service *accountability.Service, hub *hubnotify.Client) {
	t := time.NewTicker(cfg.ExpiryPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := runReminderSweep(ctx, logger, cfg, service, hub); err != nil {
				logger.Printf("reminder sweep failed: %v", err)
			}
		}
	}
}

func runReminderSweep(ctx context.Context, logger *log.Logger, cfg config, service *accountability.Service, hub *hubnotify.Client) error {
	const notifyService = "accountability-spoke"

	commitments, err := service.OverdueNeedingReminder(ctx, cfg.ReminderInterval)
	if err != nil {
		return err
	}
	for _, commitment := range commitments {
		message := formatReminderMessage(cfg, commitment)
		notifyErr := hub.Notify(ctx, hubnotify.NotifyRequest{
			Version:       hubnotify.Version2,
			TargetChannel: cfg.NotifyChannel,
			CallbackURL:   cfg.DiscordCallbackURL,
			Service:       notifyService,
			Event:         accountabilityNotifyEvent,
			Severity:      cfg.NotifySeverity,
			Title:         hubnotify.CanonicalTitle(notifyService, accountabilityNotifyEvent),
			Summary:       message,
			Fields: []hubnotify.NotifyField{
				{Key: "Task", Value: commitment.Task, Group: hubnotify.FieldGroupContext, Order: 10, Inline: false},
				{Key: "User", Value: commitment.UserID, Group: hubnotify.FieldGroupContext, Order: 20, Inline: true},
				{Key: "Deadline", Value: commitment.Deadline.UTC().Format(time.RFC3339), Group: hubnotify.FieldGroupTiming, Order: 30, Inline: true},
			},
			Actions: []hubnotify.NotifyAction{
				{ID: accountabilityActionSnooze30m, Label: "Snooze 30m", Style: hubnotify.ActionStyleSecondary},
				{ID: accountabilityActionDismiss, Label: "Dismiss", Style: hubnotify.ActionStyleSecondary},
			},
			AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{}, Users: []string{commitment.UserID}, Roles: []string{}, RepliedUser: false},
			Visibility:            hubnotify.VisibilityPublic,
			SuppressNotifications: false,
			OccurredAt:            time.Now().UTC(),
		})
		if notifyErr != nil {
			logger.Printf("failed reminder notify for commitment=%d user=%s: %v", commitment.ID, commitment.UserID, notifyErr)
			continue
		}
		if markErr := service.MarkReminderSent(ctx, commitment.ID); markErr != nil {
			logger.Printf("failed to mark reminder sent for commitment=%d: %v", commitment.ID, markErr)
		}
	}
	return nil
}

func formatReminderMessage(cfg config, commitment accountability.Commitment) string {
	base := fmt.Sprintf("<@%s> %q was due at %s.", commitment.UserID, commitment.Task, commitment.Deadline.Format(time.RFC3339))
	if !commitment.LastCheckInAt.IsZero() && strings.TrimSpace(commitment.LastCheckInText) != "" {
		base = fmt.Sprintf("%s Last check-in: %q at %s.", base, commitment.LastCheckInText, commitment.LastCheckInAt.Format(time.RFC3339))
	}

	switch {
	case commitment.ReminderCount <= 0:
		return fmt.Sprintf("%s Check in with /%s if you're moving, send /%s when done, or /%s to delay reminders.", base, cfg.CheckInCommandName, cfg.ProofCommandName, cfg.SnoozeCommandName)
	case commitment.ReminderCount == 1:
		return fmt.Sprintf("%s You still have not finished it. Send /%s when done, or /%s if you're moving now.", base, cfg.ProofCommandName, cfg.CheckInCommandName)
	default:
		return fmt.Sprintf("%s This is at risk of being missed. Send /%s now if finished.", base, cfg.ProofCommandName)
	}
}

func warnOnKnownSpokePortCollisions(logger *log.Logger) {
	knownAddrs := map[string]string{}
	for _, envName := range []string{
		"BEEMINDER_SPOKE_HTTP_ADDR",
		"FINANCE_SPOKE_HTTP_ADDR",
		"KALSHI_SPOKE_HTTP_ADDR",
		"ACCOUNTABILITY_SPOKE_HTTP_ADDR",
	} {
		if value, ok := os.LookupEnv(envName); ok {
			knownAddrs[envName] = strings.TrimSpace(value)
		}
	}
	seen := map[string]string{}
	for envName, addr := range knownAddrs {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		if prev, ok := seen[port]; ok {
			logger.Printf("warning: spoke port collision detected on %s between %s and %s", port, prev, envName)
			continue
		}
		seen[port] = envName
	}
}
