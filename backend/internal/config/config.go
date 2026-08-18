// Package config centralizes all environment-driven configuration. Every
// external integration (MongoDB, AD/LDAP, SMTP) is configurable via
// environment variables so the same binary/image can be deployed anywhere.
package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
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

// StorageConfig points at the S3-compatible object store holding attachments.
//
// Not part of the settings screen, and not by omission. Changing where the
// files live is not a setting - the objects already written do not move with
// it, so a bucket swapped from a web form turns every existing attachment into
// a broken link. It stays a deployment decision, like DATABASE_URL.
type StorageConfig struct {
	Endpoint  string
	PublicURL string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	// ForcePathStyle addresses the bucket as a path segment rather than a
	// subdomain. MinIO requires it; so does any endpoint that is an IP address.
	ForcePathStyle bool
	// URLTTLMinutes is how long a signed download link stays valid. Short
	// enough that a link pasted into a chat expires before it circulates,
	// long enough to survive a slow download of a large file.
	URLTTLMinutes int
}

// AttachmentsConfig bounds what may be uploaded.
type AttachmentsConfig struct {
	MaxBytes     int64
	MaxTaskBytes int64
	// AllowedTypes is an optional MIME allow-list ("application/pdf",
	// "image/*"). Empty allows anything the deny-list in internal/storage does
	// not refuse outright, which is the right default for an internal tool: a
	// list nobody maintains becomes the reason people go back to e-mailing
	// files around.
	AllowedTypes []string
}

// AIConfig points at an OpenAI-compatible chat completions endpoint.
//
// Managed from the settings screen rather than fixed at deploy, because which
// model an installation uses is exactly the kind of thing that changes: one
// company points this at its own inference host by address and port, another at
// a hosted provider. The wire format is the same either way, so it is a URL
// rather than a code path.
type AIConfig struct {
	Enabled  bool
	Endpoint string
	APIKey   string
	Model    string
	Timeout  int
}

// RetentionConfig bounds how long two tables nobody prunes by hand are kept.
// Zero disables a sweep entirely - the honest default, since deleting records
// is not something to start doing because a config file was left empty.
type RetentionConfig struct {
	CronExpr         string
	AuditDays        int
	NotificationDays int
}

// Config holds everything the process needs.
//
// The integrations an administrator can change from the settings screen -
// AD, SMTP, OIDC, and the two numeric thresholds - live behind accessors and
// a lock rather than as plain fields. Two reasons: a saved change has to take
// effect on the next login or the next e-mail without a restart, and a
// background goroutine reading a struct another goroutine is writing is a data
// race however harmless the values look.
//
// Everything else - the database URL, the signing secret, ports - stays a
// plain field. Those cannot be applied without a restart anyway, and a running
// installation that can rewrite its own database connection or token secret
// from a web form is one compromised administrator away from being somebody
// else's.
type Config struct {
	NodeEnv     string
	Port        string
	Log         LogConfig
	Database    DatabaseConfig
	JWT         JWTConfig
	Bootstrap   BootstrapConfig
	Storage     StorageConfig
	Attachments AttachmentsConfig
	CORSOrigin  string

	mu sync.RWMutex
	// The values as the environment supplied them. Kept so that clearing an
	// override reverts to the deployment's own configuration rather than to an
	// empty string.
	baseline  runtimeSettings
	effective runtimeSettings
}

// runtimeSettings groups the sections a settings change may replace.
type runtimeSettings struct {
	AI        AIConfig
	AD        ADConfig
	SMTP      SMTPConfig
	OIDC      OIDCConfig
	Alerts    AlertsConfig
	Retention RetentionConfig
}

// AI returns a copy of the current model settings.
func (c *Config) AI() AIConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effective.AI
}

// AD returns a copy of the current directory settings.
func (c *Config) AD() ADConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effective.AD
}

func (c *Config) SMTP() SMTPConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effective.SMTP
}

func (c *Config) OIDC() OIDCConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effective.OIDC
}

func (c *Config) Alerts() AlertsConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effective.Alerts
}

func (c *Config) Retention() RetentionConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effective.Retention
}

// Baseline reports what the environment supplied for a managed key, so the
// settings screen can show what a value would revert to if the override were
// removed.
func (c *Config) Baseline() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return flatten(c.baseline)
}

// Effective reports the values actually in force.
func (c *Config) Effective() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return flatten(c.effective)
}

// Apply replaces the managed settings with the environment baseline plus the
// given overrides. Called once at boot with whatever is stored, and again on
// every save, so the two paths cannot drift.
func (c *Config) Apply(overrides map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.effective = merge(c.baseline, overrides)
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

// getlist reads a comma-separated variable. An unset or empty value yields nil
// rather than a slice holding one empty string, which callers would otherwise
// have to special-case as "a list of nothing" versus "no list".
func getlist(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
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
	cfg := &Config{
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

		Bootstrap: BootstrapConfig{
			AdminUsername: getenv("BOOTSTRAP_ADMIN_USERNAME", "admin"),
			AdminEmail:    getenv("BOOTSTRAP_ADMIN_EMAIL", "admin@example.com"),
			AdminName:     getenv("BOOTSTRAP_ADMIN_NAME", "Administrator"),
			AdminPassword: getenv("BOOTSTRAP_ADMIN_PASSWORD", "ChangeMe123!"),
		},

		Storage: StorageConfig{
			Endpoint:  getenv("STORAGE_ENDPOINT", ""),
			PublicURL: getenv("STORAGE_PUBLIC_URL", ""),
			Region:    getenv("STORAGE_REGION", "us-east-1"),
			Bucket:    getenv("STORAGE_BUCKET", ""),
			AccessKey: getenv("STORAGE_ACCESS_KEY", ""),
			SecretKey: getenv("STORAGE_SECRET_KEY", ""),
			// Defaults to true because the bundled deployment is MinIO, which
			// has no other option, and because path style works everywhere
			// while virtual-host style does not.
			ForcePathStyle: getbool("STORAGE_FORCE_PATH_STYLE", true),
			URLTTLMinutes:  getint("STORAGE_URL_TTL_MINUTES", 15),
		},

		Attachments: AttachmentsConfig{
			// 25 MB, deliberately under the proxy's 30 MB
			// client_max_body_size. Raising this above that ceiling would move
			// enforcement to nginx, and the person uploading would get an
			// unexplained 413 instead of this application's message naming the
			// limit.
			MaxBytes:     int64(getint("ATTACHMENT_MAX_MB", 25)) << 20,
			MaxTaskBytes: int64(getint("ATTACHMENT_MAX_TASK_MB", 250)) << 20,
			AllowedTypes: getlist("ATTACHMENT_ALLOWED_TYPES"),
		},

		CORSOrigin: getenv("CORS_ORIGIN", "*"),
	}

	cfg.baseline = runtimeSettings{
		AI: AIConfig{
			Enabled:  getbool("AI_ENABLED", false),
			Endpoint: getenv("AI_ENDPOINT", ""),
			APIKey:   getenv("AI_API_KEY", ""),
			Model:    getenv("AI_MODEL", ""),
			Timeout:  getint("AI_TIMEOUT_SECONDS", 30),
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

		OIDC: OIDCConfig{
			Enabled:       getbool("OIDC_ENABLED", false),
			IssuerURL:     getenv("OIDC_ISSUER_URL", ""),
			ClientID:      getenv("OIDC_CLIENT_ID", ""),
			ClientSecret:  getenv("OIDC_CLIENT_SECRET", ""),
			RedirectURL:   getenv("OIDC_REDIRECT_URL", "https://localhost/api/auth/oidc/callback"),
			Scopes:        getenv("OIDC_SCOPES", "openid profile email"),
			AutoProvision: getbool("OIDC_AUTO_PROVISION", false),
		},

		Alerts: AlertsConfig{
			CronExpr:       getenv("ALERT_CRON", "0 * * * *"),
			WarnDaysBefore: getint("ALERT_WARN_DAYS_BEFORE", 2),
		},

		Retention: RetentionConfig{
			CronExpr:         getenv("RETENTION_CRON", "30 3 * * *"),
			AuditDays:        getint("AUDIT_RETENTION_DAYS", 0),
			NotificationDays: getint("NOTIFICATION_RETENTION_DAYS", 0),
		},
	}

	// Nothing is overridden yet: the caller loads the stored settings and
	// calls Apply once the database is reachable.
	cfg.effective = cfg.baseline
	return cfg
}
