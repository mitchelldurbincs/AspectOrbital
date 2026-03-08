package httpjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const singleObjectErr = "request body must contain a single JSON object"

// DecodeStrictJSONBody decodes a JSON request body while rejecting unknown fields
// and additional JSON values after the first payload object.
func DecodeStrictJSONBody(r *http.Request, out any, maxBytes int64) error {
	defer r.Body.Close()

	body := io.LimitReader(r.Body, maxBytes)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}

		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			return fmt.Errorf("invalid JSON payload")
		case errors.Is(err, io.ErrUnexpectedEOF):
			return fmt.Errorf("invalid JSON payload")
		case errors.As(err, &typeErr):
			if typeErr.Field != "" {
				return fmt.Errorf("invalid JSON payload: wrong type for field %q", typeErr.Field)
			}
			return fmt.Errorf("invalid JSON payload: wrong value type")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return fmt.Errorf("invalid JSON payload: %s", strings.TrimPrefix(err.Error(), "json: "))
		default:
			return fmt.Errorf("invalid JSON payload")
		}
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New(singleObjectErr)
	}

	return nil
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}
