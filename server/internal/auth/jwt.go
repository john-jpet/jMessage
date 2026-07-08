package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTTL is the session lifetime (spec MVP: 7 days).
const TokenTTL = 7 * 24 * time.Hour

// Tokens mints and verifies HS256 session JWTs.
type Tokens struct {
	secret []byte
}

func NewTokens(secret []byte) *Tokens { return &Tokens{secret: secret} }

// Mint issues a token for userID.
func (t *Tokens) Mint(userID string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

// Verify parses a token and returns the user ID it was minted for.
func (t *Tokens) Verify(token string) (string, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("auth: unexpected signing method")
		}
		return t.secret, nil
	})
	if err != nil {
		return "", err
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || claims.Subject == "" {
		return "", errors.New("auth: missing subject")
	}
	return claims.Subject, nil
}
