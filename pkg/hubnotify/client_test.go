package hubnotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotifySetsBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var payload NotifyRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		if payload.Version != Version2 || payload.Title != CanonicalTitle("beeminder-spoke", "goal-reminder") {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", server.Client())
	err := client.Notify(context.Background(), NotifyRequest{
		Version:               Version2,
		TargetChannel:         "alerts",
		Service:               "beeminder-spoke",
		Event:                 "goal-reminder",
		Severity:              SeverityInfo,
		Title:                 CanonicalTitle("beeminder-spoke", "goal-reminder"),
		Summary:               "hi",
		Fields:                []NotifyField{{Key: "Goal", Value: "study", Group: FieldGroupContext, Order: 10, Inline: false}},
		Actions:               []NotifyAction{},
		AllowedMentions:       AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
		Visibility:            VisibilityPublic,
		SuppressNotifications: false,
		OccurredAt:            time.Date(2026, time.March, 10, 14, 22, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("notify returned error: %v", err)
	}
}
