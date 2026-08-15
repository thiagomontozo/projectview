package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Password hashing
// ================
//
// New hashes use Argon2id, the current recommendation for password storage:
// unlike bcrypt it is memory-hard, so a GPU or ASIC attacker gains far less
// from parallelism.
//
// Existing bcrypt hashes stay valid. VerifyPassword accepts either format and
// reports whether the stored hash should be upgraded, so accounts migrate
// transparently the next time their owner signs in - nobody is locked out and
// no mass reset is needed.

// Parameters follow the OWASP guidance for Argon2id (19 MiB, 2 iterations).
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

var ErrInvalidHash = errors.New("unrecognized password hash format")

// HashPassword produces an Argon2id hash in the standard PHC string format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword checks a password against a stored hash of either supported
// format. needsUpgrade is true when the hash is valid but not Argon2id, which
// tells the caller to re-hash and store the result.
func VerifyPassword(hash, password string) (ok bool, needsUpgrade bool) {
	switch {
	case strings.HasPrefix(hash, "$argon2id$"):
		return verifyArgon2id(hash, password), false
	case strings.HasPrefix(hash, "$2a$"), strings.HasPrefix(hash, "$2b$"), strings.HasPrefix(hash, "$2y$"):
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
			return true, true
		}
		return false, false
	default:
		return false, false
	}
}

func verifyArgon2id(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}

	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))

	// Constant-time comparison: a timing side channel here would leak how much
	// of a guessed hash is correct.
	return subtle.ConstantTimeCompare(got, want) == 1
}
