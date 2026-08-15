package handlers

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/config"
	"projectview/internal/httpx"
	"projectview/internal/logger"
)

// Configuration an administrator can change without a redeploy.
//
// Every route here is behind RequireRole(admin) in the router, and that is the
// only place these values are reachable: the settings decide who may sign in
// and where mail goes, so being able to read them is close to being able to
// take the installation over.

// settingView describes one key to the screen. The value is present only for
// non-secrets; a secret reports whether it is set and nothing more.
type settingView struct {
	Key      string `json:"key"`
	Group    string `json:"group"`
	Kind     string `json:"kind"`
	Secret   bool   `json:"secret"`
	Value    string `json:"value,omitempty"`
	IsSet    bool   `json:"isSet"`
	Baseline string `json:"baseline,omitempty"`
	// Overridden distinguishes "the deployment configured this" from
	// "somebody changed it here", which is the first question when a value is
	// not what an operator expects.
	Overridden bool   `json:"overridden"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

// GET /api/settings
func (a *API) GetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	saved, err := a.Settings.SavedKeys(ctx)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	effective := a.Cfg.Effective()
	baseline := a.Cfg.Baseline()

	views := make([]settingView, 0, len(config.ManagedSettings()))
	for _, setting := range config.ManagedSettings() {
		stored, overridden := saved[setting.Key]
		view := settingView{
			Key: setting.Key, Group: setting.Group, Kind: setting.Kind,
			Secret: setting.Secret, Overridden: overridden,
			IsSet: effective[setting.Key] != "",
		}
		if !setting.Secret {
			view.Value = effective[setting.Key]
			view.Baseline = baseline[setting.Key]
		}
		if overridden {
			view.UpdatedAt = stored.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Key < views[j].Key })

	httpx.JSON(w, http.StatusOK, map[string]any{
		"settings": views,
		"mirror": map[string]any{
			"enabled": a.EnvMirror.Enabled(),
			"path":    a.EnvMirror.Path(),
		},
	})
}

type saveSettingsRequest struct {
	// Values to write. A secret whose value is absent or empty keeps whatever
	// is stored, so saving the form without retyping a password does not wipe
	// it - the single most common way a settings screen breaks a live system.
	Values map[string]string `json:"values"`
	// Keys to forget, reverting them to what the environment supplied.
	Clear []string `json:"clear"`
}

// PUT /api/settings
func (a *API) SaveSettings(w http.ResponseWriter, r *http.Request) {
	var req saveSettingsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	values := map[string]string{}
	changed := make([]string, 0, len(req.Values))
	for key, value := range req.Values {
		if !config.IsManaged(key) {
			// Named explicitly: a silent drop would have an administrator
			// believe they had changed something they had not.
			httpx.Error(w, http.StatusBadRequest, "This setting cannot be changed from here: "+key)
			return
		}
		if config.IsSecret(key) && strings.TrimSpace(value) == "" {
			continue
		}
		values[key] = value
		changed = append(changed, key)
	}

	for _, key := range req.Clear {
		if !config.IsManaged(key) {
			httpx.Error(w, http.StatusBadRequest, "This setting cannot be changed from here: "+key)
			return
		}
	}

	requester := auth.CurrentUser(r)
	ctx := r.Context()
	if err := a.Settings.Save(ctx, values, req.Clear, requester.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	warning := a.reloadSettings(ctx)

	sort.Strings(changed)
	// The keys, never the values: this trail is readable by every
	// administrator, and writing an SMTP password into it would undo the point
	// of encrypting it in the settings table.
	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionSettingsChanged, ResourceType: "settings",
		Changes: map[string]any{"changed": changed, "cleared": req.Clear},
		Status:  http.StatusOK,
	})

	response := map[string]any{"ok": true, "applied": true}
	if warning != "" {
		response["warning"] = warning
	}
	httpx.JSON(w, http.StatusOK, response)
}

// reloadSettings re-reads the stored overrides, applies them to the running
// process and refreshes the .env mirror. Returns a warning when the mirror
// could not be written - the save itself has already succeeded by then, and
// failing it because a bind mount is read-only would be worse than saying so.
func (a *API) reloadSettings(ctx context.Context) string {
	overrides, err := a.Settings.Overrides(ctx)
	if err != nil {
		logger.Error("settings: could not re-read overrides: %v", err)
		return "The settings were saved but could not be applied. Restart the backend."
	}
	a.Cfg.Apply(overrides)

	if a.OIDC != nil {
		// The discovery document is cached for an hour; a new issuer must not
		// keep talking to the old one.
		a.OIDC.ForgetDiscovery()
	}

	if err := a.EnvMirror.Write(a.Cfg.Effective()); err != nil {
		logger.Warn("settings: mirror not written: %v", err)
		return "Saved and applied, but the .env mirror could not be written: " + err.Error()
	}
	return ""
}

type testSettingsRequest struct {
	// Where to send the test message. Ignored by the directory test.
	To string `json:"to"`
	// Credentials to try. Only used for the directory test, and never stored.
	Username string `json:"username"`
	Password string `json:"password"`
}

// POST /api/settings/test/smtp - sends a message with the settings in force.
//
// Tests the live configuration rather than a payload, so what it proves is
// what the next real notification will do.
func (a *API) TestSMTP(w http.ResponseWriter, r *http.Request) {
	var req testSettingsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	requester := auth.CurrentUser(r)
	to := strings.TrimSpace(req.To)
	if to == "" {
		to = requester.Email
	}
	if !a.Cfg.SMTP().Enabled {
		httpx.Error(w, http.StatusBadRequest, "E-mail is switched off, so nothing would be sent.")
		return
	}

	if err := a.Mailer.Send(to, "ProjectView test message",
		"<p>This is a test from the ProjectView settings screen. If you are reading it, e-mail works.</p>"); err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "sentTo": to})
}

// POST /api/settings/test/ad - binds against the directory with the settings
// in force, using credentials supplied for the attempt and never stored.
func (a *API) TestAD(w http.ResponseWriter, r *http.Request) {
	var req testSettingsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !a.Cfg.AD().Enabled {
		httpx.Error(w, http.StatusBadRequest, "Active Directory is switched off, so nothing would be tried.")
		return
	}
	if req.Username == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "Supply a username and password to try the bind with.")
		return
	}

	profile, err := auth.AuthenticateAD(a.Cfg, req.Username, req.Password)
	if err != nil {
		// The directory's own message is useful here - "invalid credentials"
		// and "cannot reach the server" are different problems - and this
		// endpoint is administrators only.
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok": true, "username": profile.Username, "name": profile.Name, "email": profile.Email,
	})
}

// GET /api/settings/env - the mirror's contents as a download, for operators
// who want the configuration in version control rather than only in a volume.
func (a *API) DownloadEnv(w http.ResponseWriter, r *http.Request) {
	effective := a.Cfg.Effective()

	var out strings.Builder
	out.WriteString("# ProjectView settings, exported from the settings screen.\n")
	out.WriteString("# The database is authoritative; this is a snapshot.\n\n")
	for _, key := range config.ManagedKeys() {
		out.WriteString(key)
		out.WriteString("=")
		out.WriteString(effective[key])
		out.WriteString("\n")
	}

	a.Audit.Record(r, auth.CurrentUser(r), audit.Event{
		// Recorded because the file carries the directory bind password and
		// the mail account: downloading it is exfiltration if the wrong person
		// does it.
		Action: audit.ActionSettingsExported, ResourceType: "settings", Status: http.StatusOK,
	})

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="projectview.env"`)
	w.Write([]byte(out.String()))
}
