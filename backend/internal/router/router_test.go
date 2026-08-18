package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"projectview/internal/config"
	"projectview/internal/handlers"
	"projectview/internal/obs"
)

// Every API route requires a session, except the handful that cannot.
//
// This exists because of a real defect and not a hypothetical one: four route
// groups were added outside the authenticated group, and the symptom was not a
// security hole announcing itself - it was a nil pointer panic deep inside a
// permission check, because the handler asked who was calling and nothing had
// established that. The permission check itself was correct and never ran.
//
// A route added tomorrow gets the same guard for free, which is the point.
// Anything genuinely public has to be added to the list below, where somebody
// has to think about it and a reviewer can see it.
var publicRoutes = map[string]bool{
	// Signing in cannot require being signed in.
	"POST /api/auth/login":  true,
	"POST /api/auth/logout": true,
	// The access token is expected to be expired here; the refresh cookie is
	// what authenticates.
	"POST /api/auth/refresh": true,
	// Single sign-on: the provider redirects a browser that has no session yet.
	"GET /api/auth/oidc/start":    true,
	"GET /api/auth/oidc/callback": true,
	// What the login screen has to know before anybody can log in: whether this
	// installation offers directory login, single sign-on, or only a password.
	// It answers which methods exist and nothing about who exists.
	"GET /api/auth/config":      true,
	"GET /api/auth/oidc/config": true,
	// Liveness and readiness, which a load balancer reads without credentials.
	"GET /api/health":  true,
	"GET /api/ready":   true,
	"GET /api/metrics": true,
	"GET /api/version": true,
	// Intake forms, deliberately: a form somebody outside the company fills in.
	// The address is the secret, and the handler answers the questions and
	// nothing else - not the project, not its members.
	"GET /api/public/intake/{slug}":  true,
	"POST /api/public/intake/{slug}": true,
}

func TestEveryRouteRequiresASessionUnlessItIsListed(t *testing.T) {
	api := &handlers.API{Cfg: &config.Config{}}
	handler := New(api, &config.Config{}, nil, obs.NewMetrics())

	// Walking needs the concrete router, and asserting the cast keeps this test
	// honest if New ever stops returning one.
	mux, ok := handler.(chi.Routes)
	if !ok {
		t.Fatal("the router is no longer walkable, so this guard is not checking anything")
	}

	var unguarded []string
	seen := map[string]bool{}
	// The route's own handler is deliberately ignored. Calling it would skip
	// the middleware, which is the entire thing under test - and skipping it is
	// exactly how the defect behaved in production.
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/") {
			return nil
		}
		route = strings.TrimSuffix(route, "/")
		key := method + " " + route
		seen[key] = true
		if publicRoutes[key] {
			return nil
		}

		// Asked of the router rather than read off the route table: middleware
		// can be attached in several ways, and the only answer that matters is
		// what an anonymous request actually gets.
		request := httptest.NewRequest(method, concreteURL(route), nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			unguarded = append(unguarded, key+" answered "+recorder.Result().Status)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the routes: %v", err)
	}

	if len(unguarded) > 0 {
		t.Errorf("these routes answered an anonymous request:\n  %s", strings.Join(unguarded, "\n  "))
	}
}

// concreteURL fills a route pattern with values, since a request is made
// against a path rather than a pattern. The values only have to parse - the
// request never reaches a handler, because that is the thing being asserted.
func concreteURL(route string) string {
	parts := strings.Split(route, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") {
			parts[i] = "00000000-0000-0000-0000-000000000000"
		}
	}
	return strings.Join(parts, "/")
}
