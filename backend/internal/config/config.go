// Package config centralizes all environment-driven configuration. Every
// external integration (MongoDB, AD/LDAP, SMTP) is configurable via
// environment variables so the same binary/image can be deployed anywhere.
package config

import (
	"os"
	"strconv"
	"strings"
)

// DatabaseConfig points at PostgreSQL. It replaces the previous MongoConfig:
// the storage engine changed, but the principle did not - the address stays
// fully configurable through the environment.
type DatabaseConfig struct {
	URL      string
	MaxConns int
}

type JWTConfig struct {
	Secret string
	// ExpiresInHours is the access-token lifetime. It is deliberately short:
	// revocation is checked against the session record on every request, and a
	// short window bounds the damage from a leaked token even so.
	ExpiresInHours int
	// RefreshDays is how long a login survives without re-entering credentials.
	RefreshDays int
}

type ADConfig struct {
	Enabled               bool
	URL                   string
	BaseDN                string
	Domain                string
	BindDN                string
	BindPassword          string
	UsernameAttribute     string
	TLSRejectUnauthorized bool
}

type SMTPConfig struct {
	Enabled     bool
	Host        string
	Port        int
	User        string
	Password    string
	FromAddress string
}

type AlertsConfig struct {
	CronExpr       string
	WarnDaysBefore int
}

type BootstrapConfig struct {
	AdminUsername string
	AdminEmail    string
	AdminName     string
	AdminPassword string
}

// LogConfig controls the shape of the application log.
type LogConfig struct {
	Format string // "json" | "text"; empty picks json in production
	Level  string // debug | info | warn | error
}

// OIDCConfig configures single sign-on against any OpenID Connect provider.
// Endpoints are discovered from the issuer rather than configured one by one,
// so a provider that moves them does not require a redeploy.
type OIDCConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
	// AutoProvision creates an account on first successful sign-in. Off by
	// default: with it on, anyone the identity provider will authenticate has
	// an account here, which is a wider door than most organisations intend.
	AutoProvision bool
}

// RetentionConfig bounds how long two tables nobody prunes by hand are kept.
// Zero disables a sweep entirely - the honest default, since deleting records
// is not something to start doing because a config file was left empty.
type RetentionConfig struct {
	CronExpr         string
	AuditDays        int
	NotificationDays int
}

type Config struct {
	NodeEnv    string
	Port       string
	Log        LogConfig
	Database   DatabaseConfig
	JWT        JWTConfig
	AD         ADConfig
	SMTP       SMTPConfig
	Alerts     AlertsConfig
	Bootstrap  BootstrapConfig
	OIDC       OIDCConfig
	Retention  RetentionConfig
	CORSOrigin string
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getbool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getint(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// Load reads configuration from the environment (a .env file, if present,
// should be loaded by the process before calling this - see cmd/server/main.go).
func Load() *Config {
	return &Config{
		NodeEnv: getenv("NODE_ENV", "development"),
		Port:    getenv("PORT", "4000"),

		Log: LogConfig{
			Format: getenv("LOG_FORMAT", ""),
			Level:  getenv("LOG_LEVEL", "info"),
		},

		Database: DatabaseConfig{
			URL:      getenv("DATABASE_URL", "postgres://projectview:projectview@postgres:5432/projectview?sslmode=disable"),
			MaxConns: getint("DATABASE_MAX_CONNS", 10),
		},

		JWT: JWTConfig{
			Secret:         getenv("JWT_SECRET", "change-this-secret-in-production"),
			ExpiresInHours: getint("JWT_EXPIRES_IN_HOURS", 8),
			RefreshDays:    getint("SESSION_REFRESH_DAYS", 30),
		},

		AD: ADConfig{
			Enabled:               getbool("AD_ENABLED", false),
			URL:                   getenv("AD_URL", "ldap://dc.example.com:389"),
			BaseDN:                getenv("AD_BASE_DN", "dc=example,dc=com"),
			Domain:                getenv("AD_DOMAIN", "example.com"),
			BindDN:                getenv("AD_BIND_DN", ""),
			BindPassword:          getenv("AD_BIND_PASSWORD", ""),
			UsernameAttribute:     getenv("AD_USERNAME_ATTRIBUTE", "sAMAccountName"),
			TLSRejectUnauthorized: getbool("AD_TLS_REJECT_UNAUTHORIZED", true),
		},

		SMTP: SMTPConfig{
			Enabled:     getbool("SMTP_ENABLED", false),
			Host:        getenv("SMTP_HOST", "smtp.example.com"),
			Port:        getint("SMTP_PORT", 587),
			User:        getenv("SMTP_USER", ""),
			Password:    getenv("SMTP_PASSWORD", ""),
			FromAddress: getenv("SMTP_FROM", "PM Dashboard <no-reply@example.com>"),
		},

		Alerts: AlertsConfig{
			CronExpr:       getenv("ALERT_CRON", "0 * * * *"),
			WarnDaysBefore: getint("ALERT_WARN_DAYS_BEFORE", 2),
		},

		Bootstrap: BootstrapConfig{
			AdminUsername: getenv("BOOTSTRAP_ADMIN_USERNAME", "admin"),
			AdminEmail:    getenv("BOOTSTRAP_ADMIN_EMAIL", "admin@example.com"),
			AdminName:     getenv("BOOTSTRAP_ADMIN_NAME", "Administrator"),
			AdminPassword: getenv("BOOTSTRAP_ADMIN_PASSWORD", "ChangeMe123!"),
		},

		OIDC: OIDCConfig{
			Enabled:       getbool("OIDC_ENABLED", false),
			IssuerURL:     getenv("OIDC_ISSUER_URL", ""),
			ClientID:      getenv("OIDC_CLIENT_ID", ""),
			ClientSecret:  getenv("OIDC_CLIENT_SECRET", ""),
			RedirectURL:   getenv("OIDC_REDIRECT_URL", "https://localhost/api/auth/oidc/callback"),
			Scopes:        getenv("OIDC_SCOPES", "openid profile email"),
			AutoProvision: getbool("OIDC_AUTO_PROVISION", false),
		},

		Retention: RetentionConfig{
			CronExpr:         getenv("RETENTION_CRON", "30 3 * * *"),
			AuditDays:        getint("AUDIT_RETENTION_DAYS", 0),
			NotificationDays: getint("NOTIFICATION_RETENTION_DAYS", 0),
		},

		CORSOrigin: getenv("CORS_ORIGIN", "*"),
	}
}
