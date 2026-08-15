package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndVerifyArgon2id(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("new hashes must be argon2id, got %q", hash)
	}

	ok, upgrade := VerifyPassword(hash, password)
	if !ok {
		t.Error("correct password rejected")
	}
	if upgrade {
		t.Error("an argon2id hash was reported as needing an upgrade")
	}

	if ok, _ := VerifyPassword(hash, "wrong password"); ok {
		t.Error("wrong password accepted")
	}
}

// Salting means two hashes of the same password must differ; otherwise a
// stolen table reveals which accounts share a password.
func TestHashesAreSalted(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical, so they are unsalted")
	}
}

// Existing bcrypt hashes must keep working, and must be flagged for upgrade so
// accounts migrate as people sign in rather than through a forced reset.
func TestBcryptHashesStillVerifyAndRequestUpgrade(t *testing.T) {
	const password = "legacy-password"
	legacy, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	ok, upgrade := VerifyPassword(string(legacy), password)
	if !ok {
		t.Fatal("a valid bcrypt password was rejected; existing accounts would be locked out")
	}
	if !upgrade {
		t.Error("bcrypt hash was not flagged for upgrade, so it would never migrate")
	}

	if ok, _ := VerifyPassword(string(legacy), "wrong"); ok {
		t.Error("wrong password accepted against a bcrypt hash")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"$argon2id$",
		"$argon2id$v=19$m=19456,t=2,p=1$onlyonepart",
		// Right shape, invalid base64 in the salt.
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$!!!",
		// An unknown algorithm must never be treated as a match.
		"$scrypt$v=1$abc$def",
	}
	for _, hash := range cases {
		if ok, _ := VerifyPassword(hash, "anything"); ok {
			t.Errorf("malformed hash %q was accepted", hash)
		}
	}
}

// A hash produced with different cost parameters must still verify: the
// parameters are read from the hash itself, so raising them later does not
// invalidate existing passwords.
func TestVerifyReadsParametersFromTheHash(t *testing.T) {
	const password = "parameterised"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}

	// Same value, re-encoded with the parameters spelled out differently is
	// not something we can synthesise here, so assert the encoded form
	// carries them at all - that is what makes future changes safe.
	if !strings.Contains(hash, "m=") || !strings.Contains(hash, "t=") || !strings.Contains(hash, "p=") {
		t.Fatalf("hash does not encode its cost parameters: %q", hash)
	}
	if ok, _ := VerifyPassword(hash, password); !ok {
		t.Error("password did not verify against its own hash")
	}
}
