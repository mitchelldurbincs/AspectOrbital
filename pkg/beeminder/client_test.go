package beeminder

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestGetDatapointsForDayStopsAfterOlderDaystampInDescendingOrder(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if !strings.HasSuffix(r.URL.Path, "/datapoints.json") {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}

		page := r.URL.Query().Get("page")
		if page != "1" {
			t.Fatalf("unexpected page requested: %q", page)
		}

		fmt.Fprint(w, `[
			{"id":"a","timestamp":300,"daystamp":"20260309","value":1},
			{"id":"b","timestamp":200,"daystamp":"20260308","value":2}
		]`)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUsername("alice"),
		WithAuthToken("secret"),
		WithDatapointsPerPage(2),
		WithMaxDatapointPages(3),
	)

	datapoints, err := client.GetDatapointsForDay(context.Background(), "study", "20260309")
	if err != nil {
		t.Fatalf("GetDatapointsForDay() error = %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
	if len(datapoints) != 1 || datapoints[0].Daystamp != "20260309" {
		t.Fatalf("unexpected datapoints: %#v", datapoints)
	}
}

func TestGetDatapointsForDayErrorsWhenPaginationLimitExceeded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page != "1" {
			t.Fatalf("unexpected page requested: %q", page)
		}
		per := r.URL.Query().Get("per")
		if per != strconv.Itoa(2) {
			t.Fatalf("unexpected per-page query: %q", per)
		}

		fmt.Fprint(w, `[
			{"id":"a","timestamp":300,"daystamp":"20260310","value":1},
			{"id":"b","timestamp":200,"daystamp":"20260310","value":2}
		]`)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUsername("alice"),
		WithAuthToken("secret"),
		WithDatapointsPerPage(2),
		WithMaxDatapointPages(1),
	)

	_, err := client.GetDatapointsForDay(context.Background(), "study", "20260309")
	if err == nil {
		t.Fatal("expected pagination limit error")
	}
	if !strings.Contains(err.Error(), "pagination exceeded 1 pages") {
		t.Fatalf("unexpected error: %v", err)
	}
}
