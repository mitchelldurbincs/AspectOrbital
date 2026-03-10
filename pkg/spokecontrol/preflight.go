package spokecontrol

import (
	"net/http"

	"personal-infrastructure/pkg/httpjson"
)

type PreflightResult struct {
	StatusCode int
	Err        error
}

func (r *PreflightResult) Failed() bool {
	return r != nil && r.Err != nil
}

func PreflightCommand(r *http.Request, expectedToken string, out any, requestProvider func() Request) *PreflightResult {
	if r.Method != http.MethodPost {
		return &PreflightResult{StatusCode: http.StatusMethodNotAllowed, Err: errString("method not allowed")}
	}
	if !IsAuthorized(r, expectedToken) {
		return &PreflightResult{StatusCode: http.StatusUnauthorized, Err: errString("unauthorized")}
	}
	if err := httpjson.DecodeStrictJSONBody(r, out, 1<<20); err != nil {
		return &PreflightResult{StatusCode: http.StatusBadRequest, Err: err}
	}
	if err := ValidateDiscordUser(requestProvider()); err != nil {
		return &PreflightResult{StatusCode: http.StatusBadRequest, Err: err}
	}

	return nil
}

type errString string

func (e errString) Error() string {
	return string(e)
}
