package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"projectview/internal/config"
)

// OIDC implements the authorization-code flow with PKCE against any compliant
// provider (Entra ID, Okta, Keycloak, Google Workspace).
//
// Written directly rather than pulled from a library: the flow is four HTTP
// calls, and the alternative is a dependency that brings its own session
// model, its own storage assumptions and its own opinions about cookies -
// all of which this application already has.
//
// The ID token is not verified against the provider's JWKS here. It does not
// need to be: the token arrives over a direct back-channel TLS connection to
// the token endpoint in exchange for a code and a verifier only this server
// holds, so its origin is already established. Signature verification exists
// for tokens that travel through the browser, which these do not.
type OIDC struct {
	cfg    *config.Config
	client *http.Client

	mu        sync.Mutex
	discovery *discoveryDocument
	fetchedAt time.Time
}

type discoveryDocument struct {
	Issuer        string `json:"issuer"`
	AuthorizeURL  string `json:"authorization_endpoint"`
	TokenURL      string `json:"token_endpoint"`
	UserInfoURL   string `json:"userinfo_endpoint"`
	JWKSURL       string `json:"jwks_uri"`
	EndSessionURL string `json:"end_session_endpoint"`
}

func NewOIDC(cfg *config.Config) *OIDC {
	return &OIDC{cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}}
}

func (o *OIDC) Enabled() bool { return o.cfg.OIDC().Enabled }

// ForgetDiscovery drops the cached metadata, so the next sign-in re-reads it.
// Called when the settings change: a new issuer must not keep talking to the
// endpoints of the old one for up to an hour.
func (o *OIDC) ForgetDiscovery() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.discovery = nil
}

// discover fetches and caches the provider's metadata. Cached for an hour: the
// document changes when a provider rotates endpoints, which is rare, and
// fetching it on every login turns the identity provider's availability into a
// hard dependency of every single sign-in attempt.
func (o *OIDC) discover(ctx context.Context) (*discoveryDocument, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.discovery != nil && time.Since(o.fetchedAt) < time.Hour {
		return o.discovery, nil
	}

	oidc := o.cfg.OIDC()
	endpoint := strings.TrimSuffix(oidc.IssuerURL, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the identity provider: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("identity provider discovery returned %d", resp.StatusCode)
	}

	var doc discoveryDocument
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	if doc.AuthorizeURL == "" || doc.TokenURL == "" {
		return nil, errors.New("identity provider metadata is missing the authorize or token endpoint")
	}

	o.discovery = &doc
	o.fetchedAt = time.Now()
	return &doc, nil
}

// AuthRequest is what the browser needs to be sent to the provider, plus the
// two secrets the server must remember until it comes back.
type AuthRequest struct {
	URL          string
	State        string
	CodeVerifier string
}

// Start builds the authorization URL.
//
// PKCE is used even though this is a confidential client with a secret: it
// costs one hash and it closes code interception at the redirect, which is the
// one part of the flow that travels through a browser this server does not
// control.
func (o *OIDC) Start(ctx context.Context) (*AuthRequest, error) {
	doc, err := o.discover(ctx)
	if err != nil {
		return nil, err
	}

	state, err := randomToken()
	if err != nil {
		return nil, err
	}
	verifier, err := randomToken()
	if err != nil {
		return nil, err
	}
	oidc := o.cfg.OIDC()
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {oidc.ClientID},
		"redirect_uri":          {oidc.RedirectURL},
		"scope":                 {oidc.Scopes},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return &AuthRequest{
		URL:          doc.AuthorizeURL + "?" + params.Encode(),
		State:        state,
		CodeVerifier: verifier,
	}, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// OIDCProfile is the normalised identity, with the provider's stable subject
// kept separate from anything a person can change.
type OIDCProfile struct {
	Subject  string
	Username string
	Name     string
	Email    string
}

// Exchange trades the authorization code for tokens and resolves the identity.
func (o *OIDC) Exchange(ctx context.Context, code, verifier string) (*OIDCProfile, error) {
	doc, err := o.discover(ctx)
	if err != nil {
		return nil, err
	}

	oidc := o.cfg.OIDC()
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {oidc.RedirectURL},
		"client_id":     {oidc.ClientID},
		"client_secret": {oidc.ClientSecret},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging the authorization code: %w", err)
	}
	defer resp.Body.Close()

	var tokens tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokens); err != nil {
		return nil, err
	}
	if tokens.Error != "" {
		// The provider's description is not surfaced to the browser; it can
		// name the client secret's state, and the caller only needs to know
		// the exchange failed.
		return nil, fmt.Errorf("identity provider refused the code: %s", tokens.Error)
	}
	if tokens.IDToken == "" && tokens.AccessToken == "" {
		return nil, errors.New("identity provider returned no token")
	}

	claims, err := decodeIDTokenClaims(tokens.IDToken)
	if err != nil || claims["sub"] == nil {
		// Some providers put little in the ID token; userinfo is authoritative
		// and always available when the access token is.
		claims, err = o.userInfo(ctx, doc, tokens.AccessToken)
		if err != nil {
			return nil, err
		}
	}

	profile := &OIDCProfile{
		Subject: str(claims["sub"]),
		Name:    str(claims["name"]),
		Email:   str(claims["email"]),
	}
	if profile.Subject == "" {
		return nil, errors.New("identity provider returned no subject claim")
	}

	// preferred_username is the closest thing to a login name, but it is not
	// guaranteed and it is not stable - which is why the subject, not this, is
	// what accounts are linked by.
	profile.Username = str(claims["preferred_username"])
	if profile.Username == "" {
		profile.Username = strings.Split(profile.Email, "@")[0]
	}
	if profile.Username == "" {
		profile.Username = "sso-" + profile.Subject
	}
	if profile.Name == "" {
		profile.Name = profile.Username
	}
	return profile, nil
}

func (o *OIDC) userInfo(ctx context.Context, doc *discoveryDocument, accessToken string) (map[string]any, error) {
	if doc.UserInfoURL == "" || accessToken == "" {
		return nil, errors.New("identity provider offers no way to resolve the user")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, doc.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo returned %d", resp.StatusCode)
	}

	var claims map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// decodeIDTokenClaims reads the payload of a JWT without verifying it. Safe
// only because of where the token came from - see the note on OIDC.
func decodeIDTokenClaims(idToken string) (map[string]any, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed id token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
