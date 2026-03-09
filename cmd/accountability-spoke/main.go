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

	"github.com/joho/godotenv"

	"personal-infrastructure/pkg/accountability"
	"personal-infrastructure/pkg/configutil"
	"personal-infrastructure/pkg/hubnotify"
	"personal-infrastructure/pkg/lifecycle"
	"personal-infrastructure/pkg/spokecontract"
)

const (
	commandCatalogService = "accountability-spoke"
)

func main() {
	logger := log.New(os.Stdout, "accountability-spoke ", log.LstdFlags|log.Lmicroseconds)
	if err := run(logger); err != nil {
		logger.Printf("accountability-spoke exiting: %v", err)
	}
}

func run(logger *log.Logger) error {
	for _, envFile := range []string{"cmd/accountability-spoke/.env", ".env"} {
		if err := godotenv.Load(envFile); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				logger.Printf("unable to load %s: %v", envFile, err)
			}
		}
	}

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
	if _, err := service.ExpireOverdue(context.Background()); err != nil {
		logger.Printf("initial expiry sweep failed: %v", err)
	}
	if err := runReminderSweep(context.Background(), logger, cfg, service, hub); err != nil {
		logger.Printf("initial reminder sweep failed: %v", err)
	}

	app := &spokeApp{cfg: cfg, service: service, policies: policies}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", app.handleHealthz)
	mux.HandleFunc("/control/commands", app.handleCommands)
	mux.HandleFunc("/control/command", app.handleCommand)

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

type config struct {
	HTTPAddr           string
	DBPath             string
	ExpiryPollInterval time.Duration
	ExpiryGracePeriod  time.Duration
	ReminderInterval   time.Duration
	HubNotifyURL       string
	HubNotifyAuthToken string
	NotifyChannel      string
	NotifySeverity     string
	PolicyFile         string
	OpenAIBaseURL      string
	OpenAIAPIKey       string
	OpenAIModel        string
	OpenAITimeout      time.Duration
	DefaultSnooze      time.Duration
	MaxSnooze          time.Duration
	CommitCommandName  string
	ProofCommandName   string
	StatusCommandName  string
	CancelCommandName  string
	SnoozeCommandName  string
}

func loadConfig() (config, error) {
	var cfg config
	var err error

	cfg.HTTPAddr, err = configutil.StringEnvRequired("ACCOUNTABILITY_SPOKE_HTTP_ADDR")
	if err != nil {
		return config{}, err
	}

	cfg.DBPath, err = configutil.StringEnvRequired("ACCOUNTABILITY_DB_PATH")
	if err != nil {
		return config{}, err
	}

	cfg.ExpiryPollInterval, err = configutil.DurationEnvRequired("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL")
	if err != nil {
		return config{}, err
	}
	cfg.ExpiryGracePeriod, err = configutil.DurationEnvRequired("ACCOUNTABILITY_EXPIRY_GRACE_PERIOD")
	if err != nil {
		return config{}, err
	}
	cfg.ReminderInterval, err = configutil.DurationEnvRequired("ACCOUNTABILITY_REMINDER_INTERVAL")
	if err != nil {
		return config{}, err
	}
	cfg.DefaultSnooze, err = configutil.DurationEnvRequired("ACCOUNTABILITY_DEFAULT_SNOOZE")
	if err != nil {
		return config{}, err
	}
	cfg.MaxSnooze, err = configutil.DurationEnvRequired("ACCOUNTABILITY_MAX_SNOOZE")
	if err != nil {
		return config{}, err
	}
	cfg.HubNotifyURL, err = configutil.StringEnvRequired("ACCOUNTABILITY_HUB_NOTIFY_URL")
	if err != nil {
		return config{}, err
	}
	cfg.HubNotifyAuthToken, err = configutil.StringEnvRequired("ACCOUNTABILITY_HUB_NOTIFY_AUTH_TOKEN")
	if err != nil {
		return config{}, err
	}
	cfg.NotifyChannel, err = configutil.StringEnvRequired("ACCOUNTABILITY_NOTIFY_CHANNEL")
	if err != nil {
		return config{}, err
	}
	notifySeverity, err := configutil.StringEnvRequired("ACCOUNTABILITY_NOTIFY_SEVERITY")
	if err != nil {
		return config{}, err
	}
	cfg.NotifySeverity = configutil.NormalizeSeverity(notifySeverity)
	cfg.PolicyFile, err = configutil.StringEnvRequired("ACCOUNTABILITY_POLICY_FILE")
	if err != nil {
		return config{}, err
	}
	cfg.OpenAIBaseURL = configutil.StringEnv("ACCOUNTABILITY_OPENAI_BASE_URL", "https://api.openai.com/v1")
	cfg.OpenAIAPIKey = configutil.StringEnv("ACCOUNTABILITY_OPENAI_API_KEY", "")
	cfg.OpenAIModel = configutil.StringEnv("ACCOUNTABILITY_OPENAI_MODEL", "gpt-4.1-mini")
	cfg.OpenAITimeout, err = configutil.DurationEnv("ACCOUNTABILITY_OPENAI_TIMEOUT", 20*time.Second)
	if err != nil {
		return config{}, err
	}

	commitCommandName, err := configutil.StringEnvRequired("ACCOUNTABILITY_COMMAND_COMMIT")
	if err != nil {
		return config{}, err
	}
	cfg.CommitCommandName = normalizeCommand(commitCommandName)

	proofCommandName, err := configutil.StringEnvRequired("ACCOUNTABILITY_COMMAND_PROOF")
	if err != nil {
		return config{}, err
	}
	cfg.ProofCommandName = normalizeCommand(proofCommandName)

	statusCommandName, err := configutil.StringEnvRequired("ACCOUNTABILITY_COMMAND_STATUS")
	if err != nil {
		return config{}, err
	}
	cfg.StatusCommandName = normalizeCommand(statusCommandName)

	cancelCommandName, err := configutil.StringEnvRequired("ACCOUNTABILITY_COMMAND_CANCEL")
	if err != nil {
		return config{}, err
	}
	cfg.CancelCommandName = normalizeCommand(cancelCommandName)

	snoozeCommandName, err := configutil.StringEnvRequired("ACCOUNTABILITY_COMMAND_SNOOZE")
	if err != nil {
		return config{}, err
	}
	cfg.SnoozeCommandName = normalizeCommand(snoozeCommandName)

	if cfg.ExpiryPollInterval <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL must be positive")
	}
	if cfg.ExpiryGracePeriod < 0 {
		return config{}, errors.New("ACCOUNTABILITY_EXPIRY_GRACE_PERIOD must be zero or positive")
	}
	if cfg.ReminderInterval <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_REMINDER_INTERVAL must be positive")
	}
	if cfg.DefaultSnooze <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_DEFAULT_SNOOZE must be positive")
	}
	if cfg.MaxSnooze <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_MAX_SNOOZE must be positive")
	}
	if cfg.DefaultSnooze > cfg.MaxSnooze {
		return config{}, errors.New("ACCOUNTABILITY_DEFAULT_SNOOZE cannot exceed ACCOUNTABILITY_MAX_SNOOZE")
	}
	if strings.TrimSpace(cfg.PolicyFile) == "" {
		return config{}, errors.New("ACCOUNTABILITY_POLICY_FILE is required")
	}
	if cfg.OpenAITimeout <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_OPENAI_TIMEOUT must be positive")
	}
	if err := configutil.ValidateSeverity(cfg.NotifySeverity, configutil.DefaultSeverities); err != nil {
		return config{}, fmt.Errorf("ACCOUNTABILITY_NOTIFY_SEVERITY %w", err)
	}

	return cfg, nil
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
	commitments, err := service.OverdueNeedingReminder(ctx, cfg.ReminderInterval)
	if err != nil {
		return err
	}
	for _, commitment := range commitments {
		message := fmt.Sprintf("<@%s> Reminder: %q was due at %s. Submit /%s with proof or /%s to delay.", commitment.UserID, commitment.Task, commitment.Deadline.Format(time.RFC3339), cfg.ProofCommandName, cfg.SnoozeCommandName)
		notifyErr := hub.Notify(ctx, hubnotify.NotifyRequest{TargetChannel: cfg.NotifyChannel, Message: message, Severity: cfg.NotifySeverity})
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

func normalizeCommand(v string) string { return spokecontract.NormalizeCommandName(v) }
