package config

import (
	"sort"
	"strconv"
	"strings"
)

// The settings an administrator may change from the application, by the same
// names the environment uses - so a value read on the screen and a value read
// from .env are obviously the same thing.
//
// This is an allow-list, not a deny-list, and that is the whole security
// design. Anything absent here cannot be written by any request, however the
// payload is shaped. In particular DATABASE_URL, JWT_SECRET, the bootstrap
// admin credentials, PORT and CORS_ORIGIN are deliberately absent: a running
// installation that can rewrite its own database connection or token signing
// secret from a web form is one compromised administrator away from being
// somebody else's, and none of them could be applied without a restart anyway.
const (
	KindText   = "text"
	KindSecret = "secret"
	KindBool   = "bool"
	KindNumber = "number"
)

// ManagedSetting describes one editable key.
type ManagedSetting struct {
	Key string `json:"key"`
	// Group drives the sections on the settings screen.
	Group string `json:"group"`
	Kind  string `json:"kind"`
	// Secret values are never sent back to a client, only whether one is set.
	Secret bool `json:"secret"`
}

var managed = []ManagedSetting{
	{Key: "AD_ENABLED", Group: "ad", Kind: KindBool},
	{Key: "AD_URL", Group: "ad", Kind: KindText},
	{Key: "AD_BASE_DN", Group: "ad", Kind: KindText},
	{Key: "AD_DOMAIN", Group: "ad", Kind: KindText},
	{Key: "AD_BIND_DN", Group: "ad", Kind: KindText},
	{Key: "AD_BIND_PASSWORD", Group: "ad", Kind: KindSecret, Secret: true},
	{Key: "AD_USERNAME_ATTRIBUTE", Group: "ad", Kind: KindText},
	{Key: "AD_TLS_REJECT_UNAUTHORIZED", Group: "ad", Kind: KindBool},

	{Key: "SMTP_ENABLED", Group: "smtp", Kind: KindBool},
	{Key: "SMTP_HOST", Group: "smtp", Kind: KindText},
	{Key: "SMTP_PORT", Group: "smtp", Kind: KindNumber},
	{Key: "SMTP_USER", Group: "smtp", Kind: KindText},
	{Key: "SMTP_PASSWORD", Group: "smtp", Kind: KindSecret, Secret: true},
	{Key: "SMTP_FROM", Group: "smtp", Kind: KindText},

	{Key: "OIDC_ENABLED", Group: "oidc", Kind: KindBool},
	{Key: "OIDC_ISSUER_URL", Group: "oidc", Kind: KindText},
	{Key: "OIDC_CLIENT_ID", Group: "oidc", Kind: KindText},
	{Key: "OIDC_CLIENT_SECRET", Group: "oidc", Kind: KindSecret, Secret: true},
	{Key: "OIDC_REDIRECT_URL", Group: "oidc", Kind: KindText},
	{Key: "OIDC_SCOPES", Group: "oidc", Kind: KindText},
	{Key: "OIDC_AUTO_PROVISION", Group: "oidc", Kind: KindBool},

	// The cron expressions are absent on purpose: a schedule is read when the
	// scheduler is built, so changing one takes a restart. Offering a field
	// that silently does nothing until the next deploy is worse than not
	// offering it. These two are read on every run, so they apply at once.
	{Key: "ALERT_WARN_DAYS_BEFORE", Group: "alerts", Kind: KindNumber},
	{Key: "AUDIT_RETENTION_DAYS", Group: "retention", Kind: KindNumber},
	{Key: "NOTIFICATION_RETENTION_DAYS", Group: "retention", Kind: KindNumber},
}

// ManagedSettings lists the editable keys, in a stable order.
func ManagedSettings() []ManagedSetting {
	out := make([]ManagedSetting, len(managed))
	copy(out, managed)
	return out
}

// IsManaged reports whether a key may be written at all.
func IsManaged(key string) bool {
	for _, setting := range managed {
		if setting.Key == key {
			return true
		}
	}
	return false
}

// IsSecret reports whether a key's value must never be read back.
func IsSecret(key string) bool {
	for _, setting := range managed {
		if setting.Key == key {
			return setting.Secret
		}
	}
	return false
}

// ManagedKeys returns the editable key names, sorted, for the .env mirror.
func ManagedKeys() []string {
	keys := make([]string, 0, len(managed))
	for _, setting := range managed {
		keys = append(keys, setting.Key)
	}
	sort.Strings(keys)
	return keys
}

// flatten renders the settings as the string map the API and the .env mirror
// both speak, so there is one representation rather than three.
func flatten(s runtimeSettings) map[string]string {
	return map[string]string{
		"AD_ENABLED":                 strconv.FormatBool(s.AD.Enabled),
		"AD_URL":                     s.AD.URL,
		"AD_BASE_DN":                 s.AD.BaseDN,
		"AD_DOMAIN":                  s.AD.Domain,
		"AD_BIND_DN":                 s.AD.BindDN,
		"AD_BIND_PASSWORD":           s.AD.BindPassword,
		"AD_USERNAME_ATTRIBUTE":      s.AD.UsernameAttribute,
		"AD_TLS_REJECT_UNAUTHORIZED": strconv.FormatBool(s.AD.TLSRejectUnauthorized),

		"SMTP_ENABLED":  strconv.FormatBool(s.SMTP.Enabled),
		"SMTP_HOST":     s.SMTP.Host,
		"SMTP_PORT":     strconv.Itoa(s.SMTP.Port),
		"SMTP_USER":     s.SMTP.User,
		"SMTP_PASSWORD": s.SMTP.Password,
		"SMTP_FROM":     s.SMTP.FromAddress,

		"OIDC_ENABLED":        strconv.FormatBool(s.OIDC.Enabled),
		"OIDC_ISSUER_URL":     s.OIDC.IssuerURL,
		"OIDC_CLIENT_ID":      s.OIDC.ClientID,
		"OIDC_CLIENT_SECRET":  s.OIDC.ClientSecret,
		"OIDC_REDIRECT_URL":   s.OIDC.RedirectURL,
		"OIDC_SCOPES":         s.OIDC.Scopes,
		"OIDC_AUTO_PROVISION": strconv.FormatBool(s.OIDC.AutoProvision),

		"ALERT_WARN_DAYS_BEFORE":      strconv.Itoa(s.Alerts.WarnDaysBefore),
		"AUDIT_RETENTION_DAYS":        strconv.Itoa(s.Retention.AuditDays),
		"NOTIFICATION_RETENTION_DAYS": strconv.Itoa(s.Retention.NotificationDays),
	}
}

// merge lays overrides on top of the environment baseline.
//
// A key that is absent from the overrides keeps the baseline value - which is
// what makes "reset this field" mean "go back to what the deployment
// configured" rather than "go back to empty". A value that will not parse is
// ignored rather than treated as zero: a typo in a port number must not
// silently disable the mail server by making the port 0.
func merge(baseline runtimeSettings, overrides map[string]string) runtimeSettings {
	out := baseline
	if len(overrides) == 0 {
		return out
	}

	text := func(key string, target *string) {
		if raw, ok := overrides[key]; ok {
			*target = strings.TrimSpace(raw)
		}
	}
	boolean := func(key string, target *bool) {
		if raw, ok := overrides[key]; ok {
			if parsed, err := strconv.ParseBool(strings.TrimSpace(raw)); err == nil {
				*target = parsed
			}
		}
	}
	number := func(key string, target *int) {
		if raw, ok := overrides[key]; ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
				*target = parsed
			}
		}
	}

	boolean("AD_ENABLED", &out.AD.Enabled)
	text("AD_URL", &out.AD.URL)
	text("AD_BASE_DN", &out.AD.BaseDN)
	text("AD_DOMAIN", &out.AD.Domain)
	text("AD_BIND_DN", &out.AD.BindDN)
	text("AD_BIND_PASSWORD", &out.AD.BindPassword)
	text("AD_USERNAME_ATTRIBUTE", &out.AD.UsernameAttribute)
	boolean("AD_TLS_REJECT_UNAUTHORIZED", &out.AD.TLSRejectUnauthorized)

	boolean("SMTP_ENABLED", &out.SMTP.Enabled)
	text("SMTP_HOST", &out.SMTP.Host)
	number("SMTP_PORT", &out.SMTP.Port)
	text("SMTP_USER", &out.SMTP.User)
	text("SMTP_PASSWORD", &out.SMTP.Password)
	text("SMTP_FROM", &out.SMTP.FromAddress)

	boolean("OIDC_ENABLED", &out.OIDC.Enabled)
	text("OIDC_ISSUER_URL", &out.OIDC.IssuerURL)
	text("OIDC_CLIENT_ID", &out.OIDC.ClientID)
	text("OIDC_CLIENT_SECRET", &out.OIDC.ClientSecret)
	text("OIDC_REDIRECT_URL", &out.OIDC.RedirectURL)
	text("OIDC_SCOPES", &out.OIDC.Scopes)
	boolean("OIDC_AUTO_PROVISION", &out.OIDC.AutoProvision)

	number("ALERT_WARN_DAYS_BEFORE", &out.Alerts.WarnDaysBefore)
	number("AUDIT_RETENTION_DAYS", &out.Retention.AuditDays)
	number("NOTIFICATION_RETENTION_DAYS", &out.Retention.NotificationDays)

	return out
}
