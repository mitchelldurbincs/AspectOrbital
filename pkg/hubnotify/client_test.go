package hubnotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotifySetsBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", server.Client())
	err := client.Notify(context.Background(), NotifyRequest{TargetChannel: "alerts", Message: "hi", Severity: "info"})
	if err != nil {
		t.Fatalf("notify returned error: %v", err)
	}
}
