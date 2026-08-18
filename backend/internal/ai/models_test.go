package ai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Choosing the model is the part an administrator no longer does by hand, so
// it is the part that has to be predictable: the same list must always produce
// the same answer, and a model that cannot hold a conversation must never be
// the answer at all.

func TestOneModelNeedsNoChoosing(t *testing.T) {
	got, err := PickModel([]string{"llama3.1:8b"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "llama3.1:8b" {
		t.Errorf("PickModel = %q", got)
	}
}

// Sending this prompt to an embedding or a speech model is an error rather
// than a worse answer, so those are excluded before anything is ranked.
func TestModelsThatCannotChatAreNeverChosen(t *testing.T) {
	got, err := PickModel([]string{
		"text-embedding-3-large",
		"whisper-1",
		"tts-1-hd",
		"dall-e-3",
		"omni-moderation-latest",
		"nomic-embed-text",
		"gpt-4o",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "gpt-4o" {
		t.Errorf("PickModel = %q, want the only model that can chat", got)
	}

	// A server offering nothing but embeddings is a misconfiguration worth
	// naming rather than a completion that fails later for no clear reason.
	if _, err := PickModel([]string{"text-embedding-3-small", "whisper-1"}); err != ErrNoModels {
		t.Errorf("a list with no chat model gave %v, want ErrNoModels", err)
	}
	if _, err := PickModel(nil); err != ErrNoModels {
		t.Errorf("an empty list gave %v, want ErrNoModels", err)
	}
}

// The task is classifying a short request against four priorities and a
// handful of names. A large model does not do that better; it does it at
// several times the price.
func TestTheCheaperModelIsPreferred(t *testing.T) {
	got, err := PickModel([]string{"gpt-4o", "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "gpt-4o-mini" {
		t.Errorf("PickModel = %q, want the smaller model", got)
	}

	got, err = PickModel([]string{"llama3.1:70b", "llama3.2:3b"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "llama3.2:3b" {
		t.Errorf("PickModel = %q, want the smaller model", got)
	}
}

// Whatever it picks, it has to pick the same one every time. A suggestion that
// silently changed character between sweeps - because the server happened to
// list its models in a different order - would be the worst kind of bug to
// chase, since nothing would look wrong.
func TestTheChoiceDoesNotDependOnTheOrderOffered(t *testing.T) {
	forwards := []string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini", "text-embedding-3-small", "gpt-4o-mini"}
	backwards := []string{"gpt-4o-mini", "text-embedding-3-small", "gpt-4.1-mini", "gpt-4.1", "gpt-4o"}

	first, err := PickModel(forwards)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PickModel(backwards)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the same list in a different order chose %q and %q", first, second)
	}
}

// A name that was configured is the administrator's decision, and discovery
// exists to fill a gap rather than to overrule one.
func TestAConfiguredNameIsNeverOverruled(t *testing.T) {
	var listed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The base carries /v1, so the route is /v1/models - which is exactly
		// where a compatible server serves it.
		if strings.HasSuffix(r.URL.Path, "/models") {
			listed.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"id":"something-else"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ready"}}]}`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: server.URL, Model: "the-one-i-chose"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Complete(t.Context(), []Message{{Role: "user", Content: "hi"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "the-one-i-chose" {
		t.Errorf("used %q, want the configured name", result.Model)
	}
	if listed.Load() != 0 {
		t.Error("the endpoint was asked for its models although a name was configured")
	}
}

// With no name configured, the endpoint is asked - once, not on every
// completion. A sweep runs every minute and builds a fresh client each pass;
// listing models each time would be a round trip per pass for an answer that
// almost never changes.
func TestTheModelIsDiscoveredOnceAndRemembered(t *testing.T) {
	ForgetDiscovered()
	t.Cleanup(ForgetDiscovered)

	var listed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The base carries /v1, so the route is /v1/models - which is exactly
		// where a compatible server serves it.
		if strings.HasSuffix(r.URL.Path, "/models") {
			listed.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-3-small"},{"id":"qwen2.5:14b"},{"id":"qwen2.5:3b"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ready"}}]}`))
	}))
	defer server.Close()

	// A fresh client per call, exactly as the sweep does it: the cache has to
	// outlive the client or it is never used at all.
	for i := 0; i < 3; i++ {
		client, err := New(Config{Endpoint: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.Complete(t.Context(), []Message{{Role: "user", Content: "hi"}}, false)
		if err != nil {
			t.Fatalf("completion %d: %v", i, err)
		}
		if result.Model != "qwen2.5:3b" {
			t.Errorf("completion %d used %q, want the smaller chat model", i, result.Model)
		}
	}
	if listed.Load() != 1 {
		t.Errorf("the endpoint was listed %d times, want once", listed.Load())
	}

	// Saving the settings drops what was discovered, because a new endpoint
	// may not serve the name the old one did.
	ForgetDiscovered()
	client, _ := New(Config{Endpoint: server.URL})
	if _, err := client.Complete(t.Context(), []Message{{Role: "user", Content: "hi"}}, false); err != nil {
		t.Fatal(err)
	}
	if listed.Load() != 2 {
		t.Errorf("the endpoint was listed %d times after forgetting, want twice", listed.Load())
	}
}

// An endpoint with no usable model must fail as itself. Before discovery, this
// was a 404 from the completions call - which says nothing about the actual
// problem.
func TestAnEndpointWithNothingUsableSaysSo(t *testing.T) {
	ForgetDiscovered()
	t.Cleanup(ForgetDiscovered)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-3-small"}]}`))
	}))
	defer server.Close()

	client, _ := New(Config{Endpoint: server.URL})
	if _, err := client.Complete(t.Context(), []Message{{Role: "user", Content: "hi"}}, false); err != ErrNoModels {
		t.Errorf("Complete = %v, want ErrNoModels", err)
	}
}

// An endpoint alone is a complete configuration now.
func TestAnEndpointOnItsOwnIsEnough(t *testing.T) {
	client, err := New(Config{Endpoint: "192.168.1.50:8000"})
	if err != nil {
		t.Fatalf("an endpoint with no model name should be accepted: %v", err)
	}
	if !client.Enabled() {
		t.Error("the client was not built without a model name")
	}
}
