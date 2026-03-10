package hubnotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	payload := NewPublicNotifyRequest("alerts", "beeminder-spoke", "goal-reminder", SeverityInfo, "hi", time.Date(2026, time.March, 10, 14, 22, 0, 0, time.UTC))
	payload.Fields = []NotifyField{{Key: "Goal", Value: "study", Group: FieldGroupContext, Order: 10, Inline: false}}
	err := client.Notify(context.Background(), payload)
	if err != nil {
		t.Fatalf("notify returned error: %v", err)
	}
}

func TestNewPublicNotifyRequestSetsDefaults(t *testing.T) {
	payload := NewPublicNotifyRequest(" alerts ", "beeminder-spoke", "goal-reminder", SeverityWarning, " hi ", time.Date(2026, time.March, 10, 14, 22, 0, 0, time.FixedZone("EST", -5*60*60)))
	if payload.Version != Version2 || payload.TargetChannel != "alerts" {
		t.Fatalf("unexpected base payload: %#v", payload)
	}
	if payload.Title != CanonicalTitle("beeminder-spoke", "goal-reminder") || payload.Visibility != VisibilityPublic {
		t.Fatalf("unexpected derived metadata: %#v", payload)
	}
	if payload.OccurredAt.Location() != time.UTC || payload.Summary != "hi" {
		t.Fatalf("unexpected time/summary normalization: %#v", payload)
	}
	if len(payload.Actions) != 0 || !reflect.DeepEqual(payload.AllowedMentions, NoMentions()) {
		t.Fatalf("unexpected defaults: %#v", payload)
	}
}
