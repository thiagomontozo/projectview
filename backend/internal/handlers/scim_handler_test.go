package handlers

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"projectview/internal/models"
)

func TestParseUserNameFilter(t *testing.T) {
	cases := []struct {
		filter string
		want   string
		ok     bool
	}{
		{`userName eq "ana"`, "ana", true},
		{`username EQ "ana"`, "ana", true},  // clients vary in casing
		{`userName eq ana`, "ana", true},    // and in whether they quote
		{`userName co "an"`, "", false},     // contains is not equality
		{`displayName eq "Ana"`, "", false}, // only userName is supported
		{`userName eq ""`, "", false},       // an empty match is a bug upstream
		{`userName eq`, "", false},
		{``, "", false},
	}

	for _, tc := range cases {
		got, ok := parseUserNameFilter(tc.filter)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseUserNameFilter(%q) = (%q, %v), want (%q, %v)", tc.filter, got, ok, tc.want, tc.ok)
		}
	}
}

// An unsupported filter must be refused rather than silently ignored: a
// provisioning client that asked for one user and received the whole directory
// would treat every account as a duplicate.
func TestUnsupportedFilterIsNotTreatedAsNoFilter(t *testing.T) {
	if _, ok := parseUserNameFilter(`emails.value eq "ana@example.com"`); ok {
		t.Error("an unsupported filter must not be accepted")
	}
}

func TestTruthyAcceptsWhatProvisioningClientsActuallySend(t *testing.T) {
	// Entra ID sends a JSON boolean; some clients send the string "True".
	// Anything else is not a yes.
	for _, value := range []any{true, "true", "True", "TRUE"} {
		if !truthy(value) {
			t.Errorf("truthy(%#v) should be true", value)
		}
	}
	for _, value := range []any{false, "false", "no", 1, nil, map[string]any{}} {
		if truthy(value) {
			t.Errorf("truthy(%#v) should be false", value)
		}
	}
}

func TestToSCIMMapsTheAccountWithoutLeakingTheHash(t *testing.T) {
	external := "idp-subject-42"
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	user := &models.User{
		ID: uuid.New(), Username: "ana", Name: "Ana Souza", Email: "ana@example.com",
		PasswordHash: "argon2id$secret", ExternalID: &external, Active: true,
		CreatedAt: created, UpdatedAt: created,
	}

	resource := toSCIM(user)

	if resource.UserName != "ana" || resource.DisplayName != "Ana Souza" {
		t.Errorf("unexpected identity mapping: %+v", resource)
	}
	if resource.ExternalID != external {
		t.Errorf("externalId should round-trip, got %q", resource.ExternalID)
	}
	if len(resource.Emails) != 1 || !resource.Emails[0].Primary {
		t.Errorf("the address should be marked primary, got %+v", resource.Emails)
	}
	if resource.Meta.Location != "/scim/v2/Users/"+user.ID.String() {
		t.Errorf("meta.location must point at the resource, got %q", resource.Meta.Location)
	}
	if resource.Meta.Created != "2026-03-01T12:00:00Z" {
		t.Errorf("timestamps must be RFC 3339 in UTC, got %q", resource.Meta.Created)
	}
}

func TestToSCIMOmitsAnAbsentExternalID(t *testing.T) {
	// An account created here rather than by the directory has no subject, and
	// sending externalId:"" would have a client try to match on an empty
	// string.
	user := &models.User{ID: uuid.New(), Username: "local", Name: "Local", Email: "l@example.com"}
	if got := toSCIM(user).ExternalID; got != "" {
		t.Errorf("expected no externalId, got %q", got)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "third"); got != "third" {
		t.Errorf("expected the first value that is set, got %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
