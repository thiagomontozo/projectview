// Package httpx contains small helpers used across every HTTP handler for
// consistent JSON responses and error handling.
package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"projectview/internal/logger"
)

func JSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Error("failed to encode JSON response: %v", err)
	}
}

type errorBody struct {
	Message string `json:"message"`
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, errorBody{Message: message})
}

// DecodeJSON reads and decodes the request body into dst, returning a 400
// error response (and false) on failure so callers can just `return`.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return false
	}
	return true
}

// UUIDParam parses a chi URL param as a UUID, writing a 400 response (and
// returning ok=false) if it's missing or malformed.
func UUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid id: "+raw)
		return uuid.Nil, false
	}
	return id, true
}

// UUIDs converts a slice of strings to UUIDs, skipping any that fail to parse
// (defensive - the frontend always sends valid ids).
func UUIDs(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// OptionalUUID parses an id that may legitimately be absent. Returns (nil, true)
// for an empty string, meaning "explicitly cleared".
func OptionalUUID(raw string) (*uuid.UUID, bool) {
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, false
	}
	return &id, true
}
