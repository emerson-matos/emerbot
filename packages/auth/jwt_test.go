package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTSignVerifyRoundtrip(t *testing.T) {
	t.Parallel()
	j := NewJWT("super-secret")

	token, err := j.Sign("u1", "user@example.com", "Alice")
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}

	claims, err := j.Verify(token)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if claims.UserID != "u1" || claims.Email != "user@example.com" || claims.Name != "Alice" {
		t.Fatalf("claims not preserved: %+v", claims)
	}
	if claims.Subject != "u1" {
		t.Fatalf("expected subject u1, got %q", claims.Subject)
	}
}

func TestJWTVerifyRejectsWrongSecret(t *testing.T) {
	t.Parallel()
	token, err := NewJWT("right-secret").Sign("u1", "", "")
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}
	if _, err := NewJWT("wrong-secret").Verify(token); err == nil {
		t.Fatal("expected verification to fail with the wrong secret")
	}
}

func TestJWTVerifyRejectsExpired(t *testing.T) {
	t.Parallel()
	const secret = "secret"
	claims := Claims{
		UserID: "u1",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to build expired token: %v", err)
	}
	if _, err := NewJWT(secret).Verify(raw); err == nil {
		t.Fatal("expected verification to fail for an expired token")
	}
}

func TestJWTVerifyRejectsGarbage(t *testing.T) {
	t.Parallel()
	if _, err := NewJWT("secret").Verify("not-a-real-token"); err == nil {
		t.Fatal("expected verification to fail for a malformed token")
	}
}

func TestJWTVerifyRejectsNoneAlgorithm(t *testing.T) {
	t.Parallel()
	claims := Claims{
		UserID: "u1",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("failed to build alg=none token: %v", err)
	}
	if _, err := NewJWT("secret").Verify(raw); err == nil {
		t.Fatal("expected verification to reject the alg=none token")
	}
}
