package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestJSONWritesStatusAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	JSON(rec, http.StatusCreated, map[string]string{"hello": "world"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["hello"] != "world" {
		t.Errorf("body = %v", body)
	}
}

func TestErrorUsesMessageField(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, http.StatusNotFound, "Task not found.")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Message != "Task not found." {
		t.Errorf("message = %q", body.Message)
	}
}

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Title string `json:"title"`
	}

	t.Run("valid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"Ship it"}`))
		rec := httptest.NewRecorder()

		var dst payload
		if ok := DecodeJSON(rec, req, &dst); !ok {
			t.Fatal("DecodeJSON rejected a valid body")
		}
		if dst.Title != "Ship it" {
			t.Errorf("title = %q", dst.Title)
		}
	})

	t.Run("malformed body answers 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":`))
		rec := httptest.NewRecorder()

		var dst payload
		if ok := DecodeJSON(rec, req, &dst); ok {
			t.Fatal("DecodeJSON accepted a malformed body")
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}

func TestUUIDs(t *testing.T) {
	valid := uuid.New()
	other := uuid.New()

	t.Run("parses valid ids", func(t *testing.T) {
		got := UUIDs([]string{valid.String(), other.String()})
		if len(got) != 2 || got[0] != valid || got[1] != other {
			t.Errorf("UUIDs = %v, want [%v %v]", got, valid, other)
		}
	})

	t.Run("skips invalid ids instead of failing", func(t *testing.T) {
		got := UUIDs([]string{valid.String(), "not-an-id", ""})
		if len(got) != 1 || got[0] != valid {
			t.Errorf("UUIDs = %v, want [%v]", got, valid)
		}
	})

	// Membership slices are rewritten wholesale by the repositories; an empty
	// slice means "no members", which must not be confused with nil.
	t.Run("empty input yields an empty slice, not nil", func(t *testing.T) {
		got := UUIDs(nil)
		if got == nil {
			t.Fatal("UUIDs(nil) returned nil, want an empty slice")
		}
		if len(got) != 0 {
			t.Errorf("UUIDs(nil) = %v, want empty", got)
		}
	})
}

// OptionalUUID distinguishes "not supplied / cleared" from "malformed", which
// is how handlers tell a deliberate unlink from a client bug.
func TestOptionalUUID(t *testing.T) {
	id := uuid.New()

	got, ok := OptionalUUID(id.String())
	if !ok || got == nil || *got != id {
		t.Errorf("OptionalUUID(valid) = %v, %v", got, ok)
	}

	got, ok = OptionalUUID("")
	if !ok || got != nil {
		t.Errorf("OptionalUUID(\"\") = %v, %v; want nil, true (explicitly cleared)", got, ok)
	}

	if _, ok := OptionalUUID("garbage"); ok {
		t.Error("OptionalUUID accepted a malformed id")
	}
}
