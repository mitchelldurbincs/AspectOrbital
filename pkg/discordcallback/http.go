package discordcallback

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"personal-infrastructure/pkg/httpjson"
	"personal-infrastructure/pkg/hubnotify"
)

type Options struct {
	AuthToken       string
	ExpectedService string
	ExpectedEvent   string
}

type HandlerFunc func(r *http.Request, payload hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, int, error)

func HandleHTTP(w http.ResponseWriter, r *http.Request, opts Options, handler HandlerFunc) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !IsAuthorized(r, opts.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload hubnotify.ActionCallbackRequest
	if err := httpjson.DecodeStrictJSONBody(r, &payload, 1<<20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := Validate(payload, opts.ExpectedService, opts.ExpectedEvent); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, statusCode, err := handler(r, payload)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, response)
}

func IsAuthorized(r *http.Request, authToken string) bool {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	providedToken := strings.TrimSpace(parts[1])
	configuredToken := strings.TrimSpace(authToken)
	if providedToken == "" || configuredToken == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(providedToken), []byte(configuredToken)) == 1
}
