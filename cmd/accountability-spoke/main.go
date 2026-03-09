package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"personal-infrastructure/pkg/accountability"
	"personal-infrastructure/pkg/beeminder"
	"personal-infrastructure/pkg/lifecycle"
)

const (
	commandCatalogVersion = 1
	commandCatalogService = "accountability-spoke"
)

func main() {
	cfg := loadConfig()
	logger := log.New(os.Stdout, "accountability-spoke ", log.LstdFlags|log.Lmicroseconds)
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

func loadConfig() config {
	poll := 45 * time.Second
	if raw := strings.TrimSpace(os.Getenv("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			poll = d
		}
	}
	return config{
		HTTPAddr:           getenv("ACCOUNTABILITY_SPOKE_HTTP_ADDR", "127.0.0.1:8093"),
		DBPath:             getenv("ACCOUNTABILITY_DB_PATH", "file:accountability.db?_pragma=busy_timeout(5000)"),
		ExpiryPollInterval: poll,
		CommitCommandName:  normalizeCommand(getenv("ACCOUNTABILITY_COMMAND_COMMIT", "commit")),
		ProofCommandName:   normalizeCommand(getenv("ACCOUNTABILITY_COMMAND_PROOF", "proof")),
		StatusCommandName:  normalizeCommand(getenv("ACCOUNTABILITY_COMMAND_STATUS", "status")),
		CancelCommandName:  normalizeCommand(getenv("ACCOUNTABILITY_COMMAND_CANCEL", "cancel")),
		BeeminderBaseURL:   getenv("BEEMINDER_API_BASE_URL", "https://www.beeminder.com/api/v1"),
		BeeminderAuthToken: strings.TrimSpace(os.Getenv("BEEMINDER_AUTH_TOKEN")),
		BeeminderUsername:  strings.TrimSpace(os.Getenv("BEEMINDER_USERNAME")),
	}
}

func warnOnKnownSpokePortCollisions(logger *log.Logger) {
	knownAddrs := map[string]string{
		"BEEMINDER_SPOKE_HTTP_ADDR":      getenv("BEEMINDER_SPOKE_HTTP_ADDR", "127.0.0.1:8090"),
		"FINANCE_SPOKE_HTTP_ADDR":        getenv("FINANCE_SPOKE_HTTP_ADDR", "127.0.0.1:8091"),
		"KALSHI_SPOKE_HTTP_ADDR":         getenv("KALSHI_SPOKE_HTTP_ADDR", "127.0.0.1:8092"),
		"ACCOUNTABILITY_SPOKE_HTTP_ADDR": getenv("ACCOUNTABILITY_SPOKE_HTTP_ADDR", "127.0.0.1:8093"),
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
func getenv(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}
