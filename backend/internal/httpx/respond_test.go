package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
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

func TestObjectIDs(t *testing.T) {
	valid := primitive.NewObjectID()
	other := primitive.NewObjectID()

	t.Run("parses valid ids", func(t *testing.T) {
		got := ObjectIDs([]string{valid.Hex(), other.Hex()})
		if len(got) != 2 || got[0] != valid || got[1] != other {
			t.Errorf("ObjectIDs = %v, want [%v %v]", got, valid, other)
		}
	})

	t.Run("skips invalid ids instead of failing", func(t *testing.T) {
		got := ObjectIDs([]string{valid.Hex(), "not-an-id", ""})
		if len(got) != 1 || got[0] != valid {
			t.Errorf("ObjectIDs = %v, want [%v]", got, valid)
		}
	})

	// Assignees are marshalled straight into Mongo, where a nil slice and an
	// empty slice are not the same document: nil would store null and break
	// the "assignees" array queries the alert sweep relies on.
	t.Run("empty input yields an empty slice, not nil", func(t *testing.T) {
		got := ObjectIDs(nil)
		if got == nil {
			t.Fatal("ObjectIDs(nil) returned nil, want an empty slice")
		}
		if len(got) != 0 {
			t.Errorf("ObjectIDs(nil) = %v, want empty", got)
		}
	})
}
