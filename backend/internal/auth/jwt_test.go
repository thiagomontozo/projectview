package auth

import (
	"testing"

	"projectview/internal/config"
)

func testConfig(secret string, expiresInHours int) *config.Config {
	cfg := &config.Config{}
	cfg.JWT.Secret = secret
	cfg.JWT.ExpiresInHours = expiresInHours
	return cfg
}

func TestSignAndParseRoundTrip(t *testing.T) {
	cfg := testConfig("test-secret", 8)

	token, err := SignToken(cfg, "507f1f77bcf86cd799439011", "admin")
	if err != nil {
		t.Fatalf("SignToken returned an error: %v", err)
	}
	if token == "" {
		t.Fatal("SignToken returned an empty token")
	}

	claims, err := ParseToken(cfg, token)
	if err != nil {
		t.Fatalf("ParseToken rejected a freshly signed token: %v", err)
	}
	if claims.Subject != "507f1f77bcf86cd799439011" {
		t.Errorf("subject = %q, want %q", claims.Subject, "507f1f77bcf86cd799439011")
	}
	if claims.Role != "admin" {
		t.Errorf("role = %q, want %q", claims.Role, "admin")
	}
}

// A token signed with a different JWT_SECRET must never be accepted: that is
// the whole boundary between one deployment's sessions and another's.
func TestParseRejectsTokenSignedWithAnotherSecret(t *testing.T) {
	signer := testConfig("the-real-secret", 8)
	attacker := testConfig("a-different-secret", 8)

	token, err := SignToken(attacker, "507f1f77bcf86cd799439011", "admin")
	if err != nil {
		t.Fatalf("SignToken returned an error: %v", err)
	}

	if _, err := ParseToken(signer, token); err == nil {
		t.Fatal("ParseToken accepted a token signed with a different secret")
	}
}

func TestParseRejectsExpiredToken(t *testing.T) {
	// A negative lifetime produces a token that expired an hour ago.
	cfg := testConfig("test-secret", -1)

	token, err := SignToken(cfg, "507f1f77bcf86cd799439011", "member")
	if err != nil {
		t.Fatalf("SignToken returned an error: %v", err)
	}

	if _, err := ParseToken(cfg, token); err == nil {
		t.Fatal("ParseToken accepted an expired token")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	cfg := testConfig("test-secret", 8)

	for _, token := range []string{"", "not-a-jwt", "a.b.c", "Bearer something"} {
		if _, err := ParseToken(cfg, token); err == nil {
			t.Errorf("ParseToken accepted malformed token %q", token)
		}
	}
}

// "alg: none" is the classic JWT forgery: the parser must reject any token
// that is not signed with HMAC, regardless of its payload.
func TestParseRejectsUnsignedToken(t *testing.T) {
	cfg := testConfig("test-secret", 8)

	// {"alg":"none","typ":"JWT"}.{"sub":"507f...","role":"admin"}. (no signature)
	const unsigned = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJzdWIiOiI1MDdmMWY3N2JjZjg2Y2Q3OTk0MzkwMTEiLCJyb2xlIjoiYWRtaW4ifQ."

	if _, err := ParseToken(cfg, unsigned); err == nil {
		t.Fatal("ParseToken accepted an unsigned (alg=none) token")
	}
}
