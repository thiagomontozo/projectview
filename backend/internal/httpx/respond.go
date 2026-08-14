// Package httpx contains small helpers used across every HTTP handler for
// consistent JSON responses and error handling.
package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

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
		Error(w, http.StatusBadRequest, "Corpo da requisição inválido: "+err.Error())
		return false
	}
	return true
}

// ObjectIDParam parses a chi URL param as a Mongo ObjectID, writing a 400
// response (and returning ok=false) if it's missing/invalid.
func ObjectIDParam(w http.ResponseWriter, r *http.Request, name string) (primitive.ObjectID, bool) {
	raw := chi.URLParam(r, name)
	id, err := primitive.ObjectIDFromHex(raw)
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid id: "+raw)
		return primitive.NilObjectID, false
	}
	return id, true
}

// ObjectIDs converts a slice of hex strings to ObjectIDs, skipping any that
// fail to parse (defensive - the frontend always sends valid ids).
func ObjectIDs(hexes []string) []primitive.ObjectID {
	out := make([]primitive.ObjectID, 0, len(hexes))
	for _, h := range hexes {
		if id, err := primitive.ObjectIDFromHex(h); err == nil {
			out = append(out, id)
		}
	}
	return out
}
