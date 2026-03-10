package main

import (
	"net/http"

	"personal-infrastructure/pkg/httpjson"
)

func decodeJSONBody(r *http.Request, out any) error {
	return httpjson.DecodeStrictJSONBody(r, out, 1<<20)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	httpjson.WriteJSON(w, statusCode, payload)
}
