package beeminder

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetUserDecodesJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/users/alice.json"; got != want {
			t.Fatalf("unexpected path: got %q want %q", got, want)
		}
		if got := r.URL.Query().Get("auth_token"); got != "secret" {
			t.Fatalf("unexpected auth token: %q", got)
		}
		fmt.Fprint(w, `{"username":"alice","timezone":"America/New_York"}`)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUsername("alice"),
		WithAuthToken("secret"),
	)

	user, err := client.GetUser(context.Background())
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}

	if user.Username != "alice" {
		t.Fatalf("unexpected username: %q", user.Username)
	}
	if user.Timezone != "America/New_York" {
		t.Fatalf("unexpected timezone: %q", user.Timezone)
	}
}

func TestGetGoalFormatsNon2xxError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad goal slug", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUsername("alice"),
		WithAuthToken("secret"),
	)

	_, err := client.GetGoal(context.Background(), "bad-goal")
	if err == nil {
		t.Fatal("expected error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "beeminder API request failed (400 Bad Request)") {
		t.Fatalf("unexpected error format: %q", errMsg)
	}
	if !strings.Contains(errMsg, "bad goal slug") {
		t.Fatalf("error missing body: %q", errMsg)
	}
}
