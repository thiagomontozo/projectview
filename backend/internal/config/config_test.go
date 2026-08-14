package config

import "testing"

func TestGetboolAcceptedForms(t *testing.T) {
	cases := []struct {
		value    string
		fallback bool
		want     bool
	}{
		{"true", false, true},
		{"TRUE", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"on", false, true},
		{"false", true, false},
		{"0", true, false},
		{"no", true, false},
		{"off", true, false},
		// Unset and unparseable values must both fall back rather than
		// silently flipping a security-relevant flag.
		{"", true, true},
		{"", false, false},
		{"perhaps", true, true},
		{"perhaps", false, false},
	}

	for _, tc := range cases {
		t.Setenv("PV_TEST_BOOL", tc.value)
		if got := getbool("PV_TEST_BOOL", tc.fallback); got != tc.want {
			t.Errorf("getbool(%q, %v) = %v, want %v", tc.value, tc.fallback, got, tc.want)
		}
	}
}

func TestGetintFallsBackOnGarbage(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{"42", 42},
		{"0", 0},
		{"-1", -1},
		{"", 8},
		{"eight", 8},
		{"8.5", 8},
	}

	for _, tc := range cases {
		t.Setenv("PV_TEST_INT", tc.value)
		if got := getint("PV_TEST_INT", 8); got != tc.want {
			t.Errorf("getint(%q, 8) = %d, want %d", tc.value, got, tc.want)
		}
	}
}

func TestGetenvTreatsEmptyAsUnset(t *testing.T) {
	t.Setenv("PV_TEST_STR", "")
	if got := getenv("PV_TEST_STR", "fallback"); got != "fallback" {
		t.Errorf("getenv with empty value = %q, want %q", got, "fallback")
	}

	t.Setenv("PV_TEST_STR", "value")
	if got := getenv("PV_TEST_STR", "fallback"); got != "value" {
		t.Errorf("getenv = %q, want %q", got, "value")
	}
}

// The integrations must stay off unless explicitly enabled: a deployment that
// forgets to set them should not try to reach an example.com LDAP or SMTP host.
func TestLoadDefaultsKeepIntegrationsDisabled(t *testing.T) {
	for _, key := range []string{"AD_ENABLED", "SMTP_ENABLED", "DATABASE_URL", "JWT_SECRET", "ALERT_CRON", "ALERT_WARN_DAYS_BEFORE"} {
		t.Setenv(key, "")
	}

	cfg := Load()

	if cfg.AD.Enabled {
		t.Error("AD is enabled by default")
	}
	if cfg.SMTP.Enabled {
		t.Error("SMTP is enabled by default")
	}
	if cfg.Database.URL == "" {
		t.Error("DATABASE_URL has no default")
	}
	if cfg.Database.MaxConns <= 0 {
		t.Errorf("DATABASE_MAX_CONNS default = %d, want a positive number", cfg.Database.MaxConns)
	}
	if cfg.Alerts.CronExpr == "" {
		t.Error("ALERT_CRON has no default")
	}
	if cfg.Alerts.WarnDaysBefore <= 0 {
		t.Errorf("ALERT_WARN_DAYS_BEFORE default = %d, want a positive number", cfg.Alerts.WarnDaysBefore)
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db.internal:5432/custom?sslmode=require")
	t.Setenv("DATABASE_MAX_CONNS", "25")
	t.Setenv("AD_ENABLED", "true")
	t.Setenv("AD_URL", "ldaps://dc.corp.example:636")
	t.Setenv("SMTP_ENABLED", "yes")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("JWT_EXPIRES_IN_HOURS", "12")

	cfg := Load()

	if cfg.Database.URL != "postgres://db.internal:5432/custom?sslmode=require" {
		t.Errorf("Database.URL = %q", cfg.Database.URL)
	}
	if cfg.Database.MaxConns != 25 {
		t.Errorf("Database.MaxConns = %d, want 25", cfg.Database.MaxConns)
	}
	if !cfg.AD.Enabled {
		t.Error("AD.Enabled = false, want true")
	}
	if cfg.AD.URL != "ldaps://dc.corp.example:636" {
		t.Errorf("AD.URL = %q", cfg.AD.URL)
	}
	if !cfg.SMTP.Enabled {
		t.Error("SMTP.Enabled = false, want true")
	}
	if cfg.SMTP.Port != 2525 {
		t.Errorf("SMTP.Port = %d, want 2525", cfg.SMTP.Port)
	}
	if cfg.JWT.ExpiresInHours != 12 {
		t.Errorf("JWT.ExpiresInHours = %d, want 12", cfg.JWT.ExpiresInHours)
	}
}
