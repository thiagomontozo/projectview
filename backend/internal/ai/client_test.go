package ai

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The key is optional on purpose, and that is not a convenience: a model on a
// company's own network usually has no authentication at all, while a hosted
// provider rejects everything without one. The field exists either way, and an
// empty one has to mean "send no header" rather than "send an empty Bearer" -
// several compatible servers refuse the latter outright.
func TestTheKeyIsOptional(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ready"}}]}`))
	}))
	defer server.Close()

	// No key: the header must be absent entirely.
	client, err := New(Config{Endpoint: server.URL, Model: "local"})
	if err != nil {
		t.Fatalf("a configuration with no key is valid: %v", err)
	}
	if _, err := client.Complete(t.Context(), []Message{{Role: "user", Content: "hello"}}, false); err != nil {
		t.Fatalf("a request without a key failed: %v", err)
	}
	if seen != "" {
		t.Errorf("an empty key still sent %q", seen)
	}

	// A key that was supplied is sent, because a hosted provider needs it.
	withKey, err := New(Config{Endpoint: server.URL, Model: "hosted", APIKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withKey.Complete(t.Context(), []Message{{Role: "user", Content: "hello"}}, false); err != nil {
		t.Fatal(err)
	}
	if seen != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", seen)
	}
}
