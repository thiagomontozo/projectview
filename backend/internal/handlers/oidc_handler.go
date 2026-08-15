package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"projectview/internal/audit"
	"projectview/internal/httpx"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// Reasons a single sign-on can be refused after the provider itself was happy.
// Phrased for the person reading the login screen, who cannot do anything
// about either one except talk to an administrator.
var (
	errAccountDeactivated = errors.New("This account has been deactivated.")
	errNoAccount          = errors.New("No account here matches that sign-in. Ask an administrator to create one.")
)

// Single sign-on over OpenID Connect.
//
// The state and the PKCE verifier are carried in short-lived, httpOnly cookies
// rather than in server memory. Server-side storage would work on one process
// and quietly break the moment a second replica answered the callback - which
// is exactly the deployment shape this phase is meant to support.
const (
	oidcStateCookie    = "pv_oidc_state"
	oidcVerifierCookie = "pv_oidc_verifier"
	oidcStateTTL       = 10 * time.Minute
)

// GET /api/auth/oidc/config - what the login screen needs to decide whether to
// offer the button at all.
func (a *API) OIDCConfig(w http.ResponseWriter, r *http.Request) {
	enabled := a.OIDC != nil && a.OIDC.Enabled()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"enabled": enabled,
		"label":   "Single sign-on",
	})
}

// GET /api/auth/oidc/start - redirects the browser to the identity provider.
func (a *API) OIDCStart(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil || !a.OIDC.Enabled() {
		httpx.Error(w, http.StatusNotFound, "Single sign-on is not configured.")
		return
	}

	request, err := a.OIDC.Start(r.Context())
	if err != nil {
		logger.Error("oidc: could not start the flow: %v", err)
		httpx.Error(w, http.StatusBadGateway, "The identity provider could not be reached.")
		return
	}

	a.setFlowCookie(w, oidcStateCookie, request.State)
	a.setFlowCookie(w, oidcVerifierCookie, request.CodeVerifier)
	http.Redirect(w, r, request.URL, http.StatusFound)
}

// GET /api/auth/oidc/callback - the provider sends the browser back here.
func (a *API) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil || !a.OIDC.Enabled() {
		httpx.Error(w, http.StatusNotFound, "Single sign-on is not configured.")
		return
	}
	// Whatever happens next, these are spent.
	defer func() {
		a.clearFlowCookie(w, oidcStateCookie)
		a.clearFlowCookie(w, oidcVerifierCookie)
	}()

	if reason := r.URL.Query().Get("error"); reason != "" {
		a.failSSO(w, r, "The identity provider refused the sign-in.")
		return
	}

	state, err := r.Cookie(oidcStateCookie)
	verifier, verr := r.Cookie(oidcVerifierCookie)
	if err != nil || verr != nil || state.Value == "" || verifier.Value == "" {
		a.failSSO(w, r, "The sign-in took too long. Please try again.")
		return
	}
	// The state check is what stops a third party from feeding us a code they
	// obtained: without it, a login can be initiated on someone else's behalf.
	if r.URL.Query().Get("state") != state.Value {
		a.failSSO(w, r, "The sign-in could not be verified. Please try again.")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		a.failSSO(w, r, "The identity provider returned no authorization code.")
		return
	}

	profile, err := a.OIDC.Exchange(r.Context(), code, verifier.Value)
	if err != nil {
		logger.Warn("oidc: exchange failed: %v", err)
		a.failSSO(w, r, "The sign-in could not be completed.")
		return
	}

	user, err := a.resolveSSOUser(r, profile.Subject, profile.Username, profile.Name, profile.Email)
	if err != nil {
		a.Audit.RecordAnonymous(r, profile.Username, audit.Event{
			Action: audit.ActionLoginFailed, ResourceType: "session",
			Changes: map[string]any{"mode": "oidc", "reason": err.Error()},
			Status:  http.StatusForbidden,
		})
		a.failSSO(w, r, err.Error())
		return
	}

	accessToken, refreshToken, err := a.openSession(r, user)
	if err != nil {
		a.failSSO(w, r, "Failed to create the session.")
		return
	}
	a.setSessionCookies(w, accessToken, refreshToken)

	a.Audit.Record(r, user, audit.Event{
		Action: audit.ActionLogin, ResourceType: "session", ResourceID: user.ID.String(),
		Changes: map[string]any{"mode": "oidc"}, Status: http.StatusOK,
	})

	// Back to the application, not to a JSON body: the browser followed a
	// redirect chain to get here and expects a page. The session travels in
	// the cookies just set; the access token is never put in the URL, where it
	// would end up in history and in every proxy log on the way.
	http.Redirect(w, r, "/", http.StatusFound)
}

// resolveSSOUser links the provider's subject to an account.
//
// Matching is by subject first, then by e-mail for accounts that predate SSO.
// A brand-new person is only created when auto-provisioning is switched on:
// with it off, "the identity provider will authenticate them" is not the same
// statement as "they work here and should have access to this".
func (a *API) resolveSSOUser(r *http.Request, subject, username, name, email string) (*models.User, error) {
	ctx := r.Context()

	if user, err := a.Users.ByExternalID(ctx, subject); err == nil {
		if !user.Active {
			return nil, errAccountDeactivated
		}
		_ = a.Users.TouchLogin(ctx, user.ID)
		return user, nil
	}

	if email != "" {
		if user, err := a.Users.ByEmail(ctx, email); err == nil {
			if !user.Active {
				return nil, errAccountDeactivated
			}
			// Adopt the account and remember the subject, so the next sign-in
			// matches on the stable identifier rather than on an address the
			// person may change.
			if err := a.Users.Update(ctx, user.ID, repo.UserPatch{ExternalID: &subject}); err != nil {
				logger.Warn("oidc: could not link %s to its subject: %v", user.Username, err)
			}
			_ = a.Users.TouchLogin(ctx, user.ID)
			return user, nil
		}
	}

	if !a.Cfg.OIDC().AutoProvision {
		return nil, errNoAccount
	}

	user := &models.User{
		Username: username, Name: name, Email: email,
		AuthSource: models.AuthSourceOIDC, Role: models.RoleMember,
		AvatarColor: "#2a78d6", Active: true, NotifyByEmail: true,
		ExternalID: &subject,
	}
	if err := a.Users.Create(ctx, user); err != nil {
		return nil, errNoAccount
	}
	a.Audit.Record(r, user, audit.Event{
		Action: audit.ActionUserCreated, ResourceType: "user", ResourceID: user.ID.String(),
		Changes: map[string]any{"via": "oidc"}, Status: http.StatusCreated,
	})
	return user, nil
}

// failSSO returns the browser to the login screen carrying a reason, rather
// than leaving it on an API URL showing raw JSON.
func (a *API) failSSO(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(reason), http.StatusFound)
}

func (a *API) setFlowCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/api/auth/oidc",
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode,
		MaxAge: int(oidcStateTTL.Seconds()),
	})
}

func (a *API) clearFlowCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/api/auth/oidc",
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}
