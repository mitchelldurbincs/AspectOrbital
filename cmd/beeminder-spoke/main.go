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

	"github.com/joho/godotenv"

	applog "personal-infrastructure/pkg/logger"
)

const cycleTimeout = 30 * time.Second

func main() {
	logger := applog.New("beeminder-spoke")

	if err := godotenv.Load(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Printf("unable to load .env file: %v", err)
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		logger.Fatalf("invalid configuration: %v", err)
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	beeminder := newBeeminderClient(cfg, httpClient)
	hub := newHubClient(cfg, httpClient)
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

type spokeApp struct {
	cfg       config
	log       *log.Logger
	beeminder *beeminderClient
	hub       *hubClient
	engine    *reminderEngine
	location  *time.Location
}

func (a *spokeApp) runCycle(parentCtx context.Context) error {
	ctx, cancel := context.WithTimeout(parentCtx, cycleTimeout)
	defer cancel()

	nowUTC := time.Now().UTC()
	nowLocal := nowUTC.In(a.location)

	goal, err := a.beeminder.GetGoal(ctx)
	if err != nil {
		return err
	}

	target, err := dailyTargetForGoal(goal)
	if err != nil {
		return err
	}

	daystamp := beeminderDaystamp(nowLocal, goal.Deadline)
	datapoints, err := a.beeminder.GetDatapointsForDay(ctx, daystamp)
	if err != nil {
		return err
	}

	progress := aggregateDayProgress(datapoints, goal.AggDay)
	reminderStart := reminderStartForDay(nowLocal, a.cfg.ReminderStartHour, a.cfg.ReminderStartMinute)

	snapshot := dailySnapshot{
		CheckedAt:      nowUTC,
		LocalNow:       nowLocal,
		GoalSlug:       goal.Slug,
		GoalTitle:      goal.Title,
		GoalUnits:      goal.GUnits,
		GoalAggDay:     goal.AggDay,
		Daystamp:       daystamp,
		DailyTarget:    target,
		TodayProgress:  progress,
		ReminderStart:  reminderStart,
		ReminderWindow: a.cfg.ReminderInterval,
	}

	if !a.engine.Evaluate(snapshot) {
		return nil
	}

	message := renderReminderMessage(snapshot)
	if err := a.hub.Notify(ctx, hubNotifyRequest{
		TargetChannel: a.cfg.NotifyTargetChannel,
		Message:       message,
		Severity:      a.cfg.NotifySeverity,
	}); err != nil {
		return err
	}

	a.engine.MarkReminderSent(nowUTC, message)
	a.log.Printf("sent reminder for day %s: %.3f/%.3f %s", snapshot.Daystamp, snapshot.TodayProgress, snapshot.DailyTarget, snapshot.GoalUnits)

	return nil
}

func (a *spokeApp) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *spokeApp) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, a.engine.Status())
}

func (a *spokeApp) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, map[string][]string{"commands": a.cfg.Commands.All()})
}

func (a *spokeApp) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload commandRequest
	if err := decodeJSONBody(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, statusCode, err := a.executeCommand(time.Now().UTC(), payload)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *spokeApp) handleStarted(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, _, err := a.executeCommand(time.Now().UTC(), commandRequest{Command: a.cfg.Commands.Started})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *spokeApp) handleSnooze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload snoozeRequest
	if err := decodeJSONBody(r, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	argument := payload.Duration
	if argument == "" && payload.Minutes > 0 {
		argument = (time.Duration(payload.Minutes) * time.Minute).String()
	}

	result, statusCode, err := a.executeCommand(time.Now().UTC(), commandRequest{
		Command:  a.cfg.Commands.Snooze,
		Argument: argument,
	})
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *spokeApp) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, _, err := a.executeCommand(time.Now().UTC(), commandRequest{Command: a.cfg.Commands.Resume})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *spokeApp) executeCommand(now time.Time, request commandRequest) (map[string]any, int, error) {
	command := request.normalizedCommand()
	if command == "" {
		return nil, http.StatusBadRequest, errors.New("command is required")
	}

	switch command {
	case a.cfg.Commands.Started:
		until := a.engine.MarkStarted(now)
		return map[string]any{
			"status":       "ok",
			"command":      a.cfg.Commands.Started,
			"message":      fmt.Sprintf("Acknowledged. Reminders paused until %s.", until.Format(time.Kitchen)),
			"snoozedUntil": until,
		}, http.StatusOK, nil
	case a.cfg.Commands.Snooze:
		duration, err := parseSnoozeArgument(request.Argument, a.cfg.DefaultSnooze)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}

		until := a.engine.Snooze(now, duration)
		return map[string]any{
			"status":       "ok",
			"command":      a.cfg.Commands.Snooze,
			"message":      fmt.Sprintf("Snoozed reminders for %s (until %s).", duration, until.Format(time.Kitchen)),
			"duration":     duration.String(),
			"snoozedUntil": until,
		}, http.StatusOK, nil
	case a.cfg.Commands.Resume:
		a.engine.Resume(now)
		return map[string]any{
			"status":  "ok",
			"command": a.cfg.Commands.Resume,
			"message": "Reminders resumed.",
		}, http.StatusOK, nil
	case a.cfg.Commands.Status:
		status := a.engine.Status()
		return map[string]any{
			"status":  "ok",
			"command": a.cfg.Commands.Status,
			"message": summarizeStatus(status),
			"data":    status,
		}, http.StatusOK, nil
	default:
		return nil, http.StatusBadRequest, fmt.Errorf("unknown command %q; valid commands: %s", request.Command, strings.Join(a.cfg.Commands.All(), ", "))
	}
}

type commandRequest struct {
	Command  string `json:"command"`
	Argument string `json:"argument"`
}

func (c commandRequest) normalizedCommand() string {
	return normalizeCommand(c.Command)
}

type snoozeRequest struct {
	Duration string `json:"duration"`
	Minutes  int    `json:"minutes"`
}

func parseSnoozeArgument(raw string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, errors.New("snooze duration must be positive")
	}

	return duration, nil
}

func summarizeStatus(status reminderStatus) string {
	if status.Completed {
		return "Goal complete for today. No reminders are active."
	}

	if status.SnoozedUntil != nil {
		return fmt.Sprintf("Reminders are snoozed until %s.", status.SnoozedUntil.Local().Format(time.Kitchen))
	}

	if status.LastSnapshot == nil {
		return "No Beeminder snapshot yet."
	}

	remaining := status.LastSnapshot.DailyTarget - status.LastSnapshot.TodayProgress
	if remaining < 0 {
		remaining = 0
	}

	message := fmt.Sprintf(
		"Progress %.2f/%.2f %s for today (%.2f left).",
		status.LastSnapshot.TodayProgress,
		status.LastSnapshot.DailyTarget,
		status.LastSnapshot.GoalUnits,
		remaining,
	)

	if status.NextReminderAt != nil {
		message += fmt.Sprintf(" Next reminder at %s.", status.NextReminderAt.Local().Format(time.Kitchen))
	}

	return message
}

func decodeJSONBody(r *http.Request, out any) error {
	maxBodyBytes := int64(1 << 20)
	defer r.Body.Close()
	body := io.LimitReader(r.Body, maxBodyBytes)

	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}

	return nil
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}
