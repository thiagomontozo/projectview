package config

import (
	"strings"
	"testing"
)

func TestOnlyIntegrationKeysAreManaged(t *testing.T) {
	// The allow-list is the security boundary. A running installation that can
	// rewrite its own database connection or token signing secret from a web
	// form is one compromised administrator away from being somebody else's.
	for _, forbidden := range []string{
		"DATABASE_URL", "JWT_SECRET", "SESSION_REFRESH_DAYS",
		"BOOTSTRAP_ADMIN_PASSWORD", "BOOTSTRAP_ADMIN_USERNAME",
		"PORT", "NODE_ENV", "CORS_ORIGIN", "LOG_LEVEL",
	} {
		if IsManaged(forbidden) {
			t.Errorf("%s must not be settable from the application", forbidden)
		}
	}

	for _, allowed := range []string{"AD_URL", "SMTP_HOST", "OIDC_ISSUER_URL", "AUDIT_RETENTION_DAYS"} {
		if !IsManaged(allowed) {
			t.Errorf("%s should be settable", allowed)
		}
	}
}

func TestCredentialsAreMarkedSecret(t *testing.T) {
	// Anything marked secret is never read back to a client and is stored
	// encrypted, so a missed mark is a password on a screen.
	for _, key := range []string{"AD_BIND_PASSWORD", "SMTP_PASSWORD", "OIDC_CLIENT_SECRET"} {
		if !IsSecret(key) {
			t.Errorf("%s must be marked secret", key)
		}
	}
	for _, key := range []string{"AD_URL", "SMTP_HOST", "OIDC_CLIENT_ID"} {
		if IsSecret(key) {
			t.Errorf("%s is not a secret; marking it one hides it from the operator", key)
		}
	}
}

func TestEverySecretLooksLikeOne(t *testing.T) {
	// Catches a key added later whose name says password but whose flag does
	// not.
	for _, setting := range ManagedSettings() {
		looksSecret := strings.Contains(setting.Key, "PASSWORD") || strings.Contains(setting.Key, "SECRET")
		if looksSecret && !setting.Secret {
			t.Errorf("%s looks like a credential but is not marked secret", setting.Key)
		}
	}
}

func TestFlattenCoversEveryManagedKey(t *testing.T) {
	// A managed key missing from flatten would be editable but never applied,
	// and never written to the mirror.
	values := flatten(runtimeSettings{})
	for _, setting := range ManagedSettings() {
		if _, ok := values[setting.Key]; !ok {
			t.Errorf("%s is managed but flatten does not produce it", setting.Key)
		}
	}
}

func TestMergeKeepsTheBaselineForAbsentKeys(t *testing.T) {
	baseline := runtimeSettings{}
	baseline.AD.URL = "ldap://from-env:389"
	baseline.SMTP.Host = "smtp.env"

	merged := merge(baseline, map[string]string{"SMTP_HOST": "smtp.override"})

	if merged.SMTP.Host != "smtp.override" {
		t.Errorf("the override should win, got %q", merged.SMTP.Host)
	}
	// Clearing an override means "go back to what the deployment configured",
	// not "go back to empty".
	if merged.AD.URL != "ldap://from-env:389" {
		t.Errorf("an untouched key should keep the environment value, got %q", merged.AD.URL)
	}
}

func TestMergeIgnoresValuesThatWillNotParse(t *testing.T) {
	baseline := runtimeSettings{}
	baseline.SMTP.Port = 587
	baseline.AD.Enabled = true

	// A typo in a port must not silently disable the mail server by making the
	// port 0, and a nonsense boolean must not switch the directory off.
	merged := merge(baseline, map[string]string{
		"SMTP_PORT":  "five-eight-seven",
		"AD_ENABLED": "perhaps",
	})

	if merged.SMTP.Port != 587 {
		t.Errorf("an unparseable port should be ignored, got %d", merged.SMTP.Port)
	}
	if !merged.AD.Enabled {
		t.Error("an unparseable boolean should be ignored")
	}
}

func TestMergeTrimsWhitespace(t *testing.T) {
	// A hostname pasted with a trailing space is the classic support ticket.
	merged := merge(runtimeSettings{}, map[string]string{"SMTP_HOST": "  smtp.example.com  "})
	if merged.SMTP.Host != "smtp.example.com" {
		t.Errorf("expected the value trimmed, got %q", merged.SMTP.Host)
	}
}

func TestApplyIsReversible(t *testing.T) {
	cfg := &Config{}
	cfg.baseline.SMTP.Host = "smtp.env"
	cfg.effective = cfg.baseline

	cfg.Apply(map[string]string{"SMTP_HOST": "smtp.saved"})
	if cfg.SMTP().Host != "smtp.saved" {
		t.Fatalf("expected the override in force, got %q", cfg.SMTP().Host)
	}

	// Removing the stored row and applying again must restore the deployment's
	// own value rather than leave the last override in place.
	cfg.Apply(map[string]string{})
	if cfg.SMTP().Host != "smtp.env" {
		t.Errorf("expected the baseline restored, got %q", cfg.SMTP().Host)
	}
}
