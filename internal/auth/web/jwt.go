package web

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = 30 * 24 * time.Hour

type Claims struct {
	UserID int64 `json:"uid"`
	jwt.RegisteredClaims
}

func IssueToken(secret []byte, userID int64) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(tokenTTL)
	c := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := t.SignedString(secret)
	return s, exp, err
}

func ParseToken(secret []byte, raw string) (int64, error) {
	t, err := jwt.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected alg")
		}
		return secret, nil
	})
	if err != nil {
		return 0, err
	}
	c, ok := t.Claims.(*Claims)
	if !ok || !t.Valid {
		return 0, errors.New("invalid token")
	}
	return c.UserID, nil
}
