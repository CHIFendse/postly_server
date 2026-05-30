package jwt

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

func Issue(userID string) (string, error) {
	c := &claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(72 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(key())
}

func Parse(raw string) (string, error) {
	tok, err := jwt.ParseWithClaims(raw, &claims{}, func(*jwt.Token) (any, error) {
		return key(), nil
	})
	if err != nil || !tok.Valid {
		return "", errors.New("invalid token")
	}
	c, ok := tok.Claims.(*claims)
	if !ok {
		return "", errors.New("invalid claims")
	}
	return c.UserID, nil
}

func key() []byte { return []byte(os.Getenv("JWT_SECRET")) }
