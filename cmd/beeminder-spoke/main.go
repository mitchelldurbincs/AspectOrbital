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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

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

	writeJSON(w, http.StatusOK, commandCatalogForConfig(a.cfg))
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
			"message":      fmt.Sprintf("Got it. Paused reminders until %s.", formatClockInLocation(until, a.location)),
			"snoozedUntil": until,
		}, http.StatusOK, nil
	case a.cfg.Commands.Snooze:
		durationInput := request.optionString(snoozeDurationOptionName)
		if durationInput == "" {
			durationInput = request.Argument
		}

		duration, err := parseSnoozeArgument(durationInput, a.cfg.DefaultSnooze)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}

		until := a.engine.Snooze(now, duration)
		return map[string]any{
			"status":       "ok",
			"command":      a.cfg.Commands.Snooze,
			"message":      fmt.Sprintf("Snoozed reminders for %s (until %s).", duration, formatClockInLocation(until, a.location)),
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
			"message": summarizeStatus(status, a.location),
			"data":    status,
		}, http.StatusOK, nil
	default:
		return nil, http.StatusBadRequest, fmt.Errorf("unknown command %q; valid commands: %s", request.Command, strings.Join(a.cfg.Commands.All(), ", "))
	}
}

type commandRequest struct {
	Command  string         `json:"command"`
	Argument string         `json:"argument"`
	Options  map[string]any `json:"options,omitempty"`
}

func (c commandRequest) normalizedCommand() string {
	return normalizeCommand(c.Command)
}

func (c commandRequest) optionString(name string) string {
	if c.Options == nil {
		return ""
	}

	raw, ok := c.Options[name]
	if !ok {
		return ""
	}

	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return strings.TrimSpace(value.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 64))
	case bool:
		return strings.TrimSpace(strconv.FormatBool(value))
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

type commandCatalogResponse struct {
	Version  int                 `json:"version"`
	Service  string              `json:"service"`
	Commands []commandDefinition `json:"commands"`
	Names    []string            `json:"commandNames"`
}

type commandDefinition struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Options     []commandOptionDefinition `json:"options,omitempty"`
}

type commandOptionDefinition struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func commandCatalogForConfig(cfg config) commandCatalogResponse {
	commands := []commandDefinition{
		{
			Name:        cfg.Commands.Status,
			Description: "Show progress and next reminder time",
		},
		{
			Name:        cfg.Commands.Snooze,
			Description: "Pause reminders for a duration",
			Options: []commandOptionDefinition{
				{
					Name:        snoozeDurationOptionName,
					Type:        snoozeDurationOptionType,
					Description: snoozeDurationOptionPrompt,
					Required:    false,
				},
			},
		},
		{
			Name:        cfg.Commands.Started,
			Description: "Pause reminders while you get started",
		},
		{
			Name:        cfg.Commands.Resume,
			Description: "Resume reminders immediately",
		},
	}

	return commandCatalogResponse{
		Version:  commandCatalogVersion,
		Service:  commandCatalogService,
		Commands: commands,
		Names:    cfg.Commands.All(),
	}
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
		return 0, fmt.Errorf("invalid duration %q; try values like 15m or 1h", value)
	}
	if duration <= 0 {
		return 0, errors.New("snooze duration must be positive")
	}

	return duration, nil
}

func summarizeStatus(status reminderStatus, location *time.Location) string {
	if status.Completed {
		return "Goal complete for today. No reminders are active."
	}

	if status.LastSnapshot == nil {
		if status.SnoozedUntil != nil {
			return fmt.Sprintf("No snapshot yet. Reminders are snoozed until %s.", formatClockInLocation(*status.SnoozedUntil, location))
		}

		return "No Beeminder snapshot yet."
	}

	done := status.LastSnapshot.TodayProgress
	target := status.LastSnapshot.DailyTarget
	remaining := target - done
	if remaining < 0 {
		remaining = 0
	}

	unit := strings.TrimSpace(status.LastSnapshot.GoalUnits)
	if unit == "" {
		unit = "units"
	}
	decimals := 2
	if looksLikeHours(unit) {
		done *= 60
		target *= 60
		remaining *= 60
		unit = "min"
		decimals = 1
	}

	message := fmt.Sprintf(
		"Today: %s/%s %s, %s %s left.",
		formatFloat(done, decimals),
		formatFloat(target, decimals),
		unit,
		formatFloat(remaining, decimals),
		unit,
	)

	nextPingAt := resolveNextPingTime(status)
	if !nextPingAt.IsZero() {
		message += fmt.Sprintf(" Next ping: %s.", formatClockInLocation(nextPingAt, location))
	}

	return message
}

func resolveNextPingTime(status reminderStatus) time.Time {
	if status.SnoozedUntil != nil {
		return *status.SnoozedUntil
	}

	if status.NextReminderAt != nil {
		return *status.NextReminderAt
	}

	if status.LastSnapshot == nil {
		return time.Time{}
	}

	if status.LastSnapshot.LocalNow.Before(status.LastSnapshot.ReminderStart) {
		return status.LastSnapshot.ReminderStart
	}

	if status.LastReminderAt != nil && status.LastSnapshot.ReminderWindow > 0 {
		return status.LastReminderAt.Add(status.LastSnapshot.ReminderWindow)
	}

	return status.LastSnapshot.LocalNow
}

func formatClockInLocation(value time.Time, location *time.Location) string {
	if location != nil {
		value = value.In(location)
	} else {
		value = value.Local()
	}

	return value.Format("3:04 PM MST")
}

func formatFloat(value float64, decimals int) string {
	return strconv.FormatFloat(value, 'f', decimals, 64)
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
