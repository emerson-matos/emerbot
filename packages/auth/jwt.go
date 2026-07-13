package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are the JWT payload fields.
type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	jwt.RegisteredClaims
}

const (
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 7 * 24 * time.Hour
)

// TokenPair holds the access and refresh tokens returned on login.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds
}

// JWT signs and verifies tokens using HS256 with the provided secret.
type JWT struct {
	secret []byte
}

func NewJWT(secret string) *JWT {
	return &JWT{secret: []byte(secret)}
}

// Sign creates an access token for the given user.
func (j *JWT) Sign(userID, email, name string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Email:  email,
		Name:   name,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// Verify parses and validates a token, returning its claims.
func (j *JWT) Verify(tokenString string) (Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return j.secret, nil
	})
	if err != nil {
		return Claims{}, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return Claims{}, errors.New("invalid token claims")
	}
	return *claims, nil
}

// RefreshTokenTTL exposes the refresh TTL for token creation.
func RefreshTokenTTL() time.Duration { return refreshTokenTTL }
