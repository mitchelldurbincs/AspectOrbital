package main

import (
	"context"
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
	"personal-infrastructure/pkg/beeminder"
	"personal-infrastructure/pkg/configutil"
	"personal-infrastructure/pkg/lifecycle"
)

const (
	commandCatalogVersion = 1
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

	if err := accountability.Bootstrap(context.Background(), cfg.DBPath); err != nil {
		logger.Fatalf("bootstrap db: %v", err)
	}

	var bee *beeminder.Client
	if cfg.BeeminderAuthToken != "" && cfg.BeeminderUsername != "" {
		bee = beeminder.NewClient(
			beeminder.WithBaseURL(cfg.BeeminderBaseURL),
			beeminder.WithAuthToken(cfg.BeeminderAuthToken),
			beeminder.WithUsername(cfg.BeeminderUsername),
		)
	}
	var beeminderWriter accountability.BeeminderWriter
	if bee != nil {
		beeminderWriter = beeminderClientAdapter{client: bee}
	}
	service := accountability.NewService(cfg.DBPath, beeminderWriter, cfg.ExpiryPollInterval)

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()
	go service.StartExpiryLoop(loopCtx)
	if _, err := service.ExpireOverdue(context.Background()); err != nil {
		logger.Printf("initial expiry sweep failed: %v", err)
	}

	app := &spokeApp{cfg: cfg, service: service}
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

type beeminderClientAdapter struct {
	client *beeminder.Client
}

func (a beeminderClientAdapter) CreateDatapoint(ctx context.Context, datapoint accountability.Datapoint) error {
	if a.client == nil {
		return nil
	}
	return a.client.CreateDatapoint(ctx, beeminder.DatapointRequest{
		GoalSlug: datapoint.GoalSlug,
		Value:    datapoint.Value,
		Comment:  datapoint.Comment,
		Time:     datapoint.Time,
	})
}

type config struct {
	HTTPAddr           string
	DBPath             string
	ExpiryPollInterval time.Duration
	CommitCommandName  string
	ProofCommandName   string
	StatusCommandName  string
	CancelCommandName  string
	BeeminderBaseURL   string
	BeeminderAuthToken string
	BeeminderUsername  string
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

	cfg.BeeminderBaseURL, err = configutil.StringEnvRequired("BEEMINDER_API_BASE_URL")
	if err != nil {
		return config{}, err
	}

	cfg.BeeminderAuthToken, err = configutil.StringEnvRequired("BEEMINDER_AUTH_TOKEN")
	if err != nil {
		return config{}, err
	}

	cfg.BeeminderUsername, err = configutil.StringEnvRequired("BEEMINDER_USERNAME")
	if err != nil {
		return config{}, err
	}

	if cfg.ExpiryPollInterval <= 0 {
		return config{}, errors.New("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL must be positive")
	}

	return cfg, nil
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

func normalizeCommand(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
