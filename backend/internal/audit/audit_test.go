package audit

import (
	"reflect"
	"testing"
)

// The audit table is widely readable by design, so anything secret must be
// stripped before it lands there. A leak here would be worse than no trail.
func TestRedactRemovesSecrets(t *testing.T) {
	in := map[string]any{
		"username":        "ana",
		"password":        "hunter2",
		"Password":        "hunter2",
		"passwordHash":    "$argon2id$...",
		"password_hash":   "$argon2id$...",
		"currentPassword": "hunter2",
		"token":           "abc",
		"refreshToken":    "def",
		"Authorization":   "Bearer xyz",
		"apiKey":          "k",
		"api_key":         "k",
		"bindPassword":    "svc",
		"role":            "admin",
	}

	out := Redact(in)

	for _, key := range []string{
		"password", "Password", "passwordHash", "password_hash", "currentPassword",
		"token", "refreshToken", "Authorization", "apiKey", "api_key", "bindPassword",
	} {
		if out[key] != "[redacted]" {
			t.Errorf("key %q was not redacted, got %v", key, out[key])
		}
	}

	// Non-sensitive fields must survive, or the trail records nothing useful.
	if out["username"] != "ana" {
		t.Errorf("username was altered: %v", out["username"])
	}
	if out["role"] != "admin" {
		t.Errorf("role was altered: %v", out["role"])
	}
}

func TestRedactIsRecursive(t *testing.T) {
	in := map[string]any{
		"user": map[string]any{
			"name":     "ana",
			"password": "hunter2",
		},
	}
	out := Redact(in)

	nested, ok := out["user"].(map[string]any)
	if !ok {
		t.Fatalf("nested map lost its type: %T", out["user"])
	}
	if nested["password"] != "[redacted]" {
		t.Error("nested password was not redacted")
	}
	if nested["name"] != "ana" {
		t.Error("nested non-sensitive field was altered")
	}
}

func TestRedactHandlesNil(t *testing.T) {
	if Redact(nil) != nil {
		t.Error("Redact(nil) should stay nil so the column is left NULL")
	}
}

// The trail should record the change, not the whole record.
func TestDiffOnlyReportsChanges(t *testing.T) {
	before := map[string]any{"name": "Old", "status": "active", "color": "#fff"}
	after := map[string]any{"name": "New", "status": "active", "color": "#fff"}

	changes := Diff(before, after)

	want := map[string]any{"name": map[string]any{"from": "Old", "to": "New"}}
	if !reflect.DeepEqual(changes, want) {
		t.Errorf("Diff = %v, want %v", changes, want)
	}
}

func TestDiffReturnsNilWhenNothingChanged(t *testing.T) {
	same := map[string]any{"name": "Same", "status": "active"}
	if changes := Diff(same, same); changes != nil {
		t.Errorf("Diff of identical maps = %v, want nil", changes)
	}
}

func TestDiffReportsNewFields(t *testing.T) {
	changes := Diff(map[string]any{}, map[string]any{"archived": true})
	entry, ok := changes["archived"].(map[string]any)
	if !ok {
		t.Fatalf("missing entry for a newly set field: %v", changes)
	}
	if entry["from"] != nil || entry["to"] != true {
		t.Errorf("unexpected entry %v", entry)
	}
}

// A nil Recorder must be safe: tests and code paths without a database should
// not have to guard every call site.
func TestNilRecorderIsSafe(t *testing.T) {
	var rec *Recorder
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil Recorder panicked: %v", r)
		}
	}()
	rec.Record(nil, nil, Event{Action: ActionLogin})
	rec.RecordAnonymous(nil, "someone", Event{Action: ActionLoginFailed})
}
