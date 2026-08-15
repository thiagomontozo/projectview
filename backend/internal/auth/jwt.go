package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"projectview/internal/config"
)

// Claims carries the session id alongside the subject. That id is what makes
// revocation possible: the middleware checks it against the sessions table on
// every request, so ending a session takes effect immediately instead of
// whenever the token happens to expire.
type Claims struct {
	Subject   string `json:"sub"`
	Role      string `json:"role"`
	SessionID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

func SignToken(cfg *config.Config, userID, role string, sessionID uuid.UUID) (string, error) {
	claims := Claims{
		Subject:   userID,
		Role:      role,
		SessionID: sessionID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(cfg.JWT.ExpiresInHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWT.Secret))
}

func ParseToken(cfg *config.Config, tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(cfg.JWT.Secret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired token")
	}
	return claims, nil
}

// SessionUUID returns the session this token was minted from. Tokens issued
// before sessions existed carry none, and are rejected by the middleware.
func (c *Claims) SessionUUID() (uuid.UUID, bool) {
	if c.SessionID == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(c.SessionID)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
