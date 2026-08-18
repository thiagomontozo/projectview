package services

import (
	"testing"

	"projectview/internal/config"
)

// The two properties that are not obvious from reading the sweep, and that
// would both fail silently.

// With no model configured, a pass must not touch the database at all. The
// repositories are nil here on purpose: if the sweep reaches one, this panics
// rather than quietly passing.
func TestASweepWithNoModelDoesNothing(t *testing.T) {
	sweep := NewTriageSweep(&config.Config{}, nil, nil, nil)

	if got := sweep.Run(t.Context()); got != 0 {
		t.Errorf("Run with no model triaged %d submission(s)", got)
	}
}

// The settings are editable from the administration screen and are promised to
// apply without a restart, so the sweep has to see a change made after it
// started. A client captured at construction would fail this - and would fail
// it in the field as "I turned it on and nothing happened".
func TestTurningTheModelOnTakesEffectWithoutARestart(t *testing.T) {
	cfg := &config.Config{}
	sweep := NewTriageSweep(cfg, nil, nil, nil)

	if sweep.client().Enabled() {
		t.Fatal("a model was configured before anything was set")
	}

	cfg.Apply(map[string]string{
		"AI_ENABLED": "true",
		// Deliberately a bare address with no scheme and no path: what an
		// administrator types is what has to work.
		"AI_ENDPOINT": "192.168.1.50:8000",
		"AI_MODEL":    "llama3.1",
	})

	client := sweep.client()
	if !client.Enabled() {
		t.Fatal("the sweep did not pick up the model that was just turned on")
	}
	if client.Model() != "llama3.1" {
		t.Errorf("model = %q, want llama3.1", client.Model())
	}
}
