package main

import (
	"io"
	"log"
	"net/http"
	"time"

	"personal-infrastructure/pkg/hubnotify"
)

func testConfig() config {
	return config{
		BeeminderAuthToken:  "token",
		BeeminderUsername:   "alice",
		BeeminderGoalSlugs:  []string{"study"},
		DiscordCallbackURL:  "http://127.0.0.1:8090/discord/callback",
		CallbackAuthToken:   "test-callback-token",
		NotifyTargetChannel: "beeminder",
		NotifySeverity:      "info",
		ReminderInterval:    30 * time.Minute,
		ReminderStartHour:   0,
		ReminderStartMinute: 0,
		ActiveGrace:         15 * time.Minute,
		StartedSnooze:       20 * time.Minute,
		DefaultSnooze:       30 * time.Minute,
		MaxSnooze:           2 * time.Hour,
		RequireDailyRate:    true,
		Commands:            controlCommands{Started: "started", Snooze: "b-snooze", Resume: "resume", Status: "status"},
		DatapointsPerPage:   100,
		MaxDatapointPages:   20,
		HTTPTimeout:         5 * time.Second,
	}
}

func newTestApp(cfg config) *spokeApp {
	return &spokeApp{
		cfg:      cfg,
		log:      log.New(io.Discard, "", 0),
		engine:   newReminderEngine(cfg),
		location: time.UTC,
	}
}

func newTestAppWithClients(cfg config, httpClient *http.Client, hubURL string) *spokeApp {
	app := newTestApp(cfg)
	app.beeminder = newBeeminderClient(cfg, httpClient)
	app.hub = hubnotify.NewClient(hubURL, "hub-token", httpClient)
	return app
}
