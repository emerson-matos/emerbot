package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/emerson/emerbot/packages/shared"
)

// NewLocalCognitoMiddleware is the only place in the app that verifies a token
// itself (the deployed path delegates to API Gateway's authorizer), so every
// check it makes is tested here against a real JWKS endpoint and real signed
// tokens — an accepted-but-forged token is the failure mode that matters.

const (
	testIssuer   = "https://cognito-idp.test.local/pool"
	testClientID = "test-client-id"
	testKID      = "test-kid"
)

func newJWKSServer(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
	body, err := json.Marshal(map[string]any{
		"keys": []map[string]any{
			{"kty": "RSA", "use": "sig", "kid": testKID, "alg": "RS256", "n": n, "e": e},
		},
	})
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// mintToken signs a Cognito-shaped ID token, with overrides applied last so a
// test can corrupt exactly one claim.
func mintToken(t *testing.T, key *rsa.PrivateKey, kid string, overrides jwt.MapClaims) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":          "cognito-sub-1",
		"email":        "ana@loja.com",
		"name":         "Ana",
		"phone_number": "+5511987654321",
		"token_use":    "id",
		"aud":          testClientID,
		"iss":          testIssuer,
		"iat":          now.Unix(),
		"exp":          now.Add(time.Hour).Unix(),
	}
	for k, v := range overrides {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// newMiddleware builds the middleware against a JWKS server holding key's
// public half, and returns it wrapped around a claims recorder.
func newMiddleware(t *testing.T, key *rsa.PrivateKey) (http.Handler, *Claims, *bool) {
	t.Helper()
	srv := newJWKSServer(t, key)
	mw, err := NewLocalCognitoMiddleware(context.Background(), srv.URL, testIssuer, testClientID)
	if err != nil {
		t.Fatalf("build middleware: %v", err)
	}
	seen := &Claims{}
	called := new(bool)
	return mw(nextRecorder(seen, called)), seen, called
}

func doRequest(h http.Handler, authorization string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/entries", nil)
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLocalCognitoAcceptsAValidIDToken(t *testing.T) {
	key := newKey(t)
	h, seen, called := newMiddleware(t, key)

	w := doRequest(h, "Bearer "+mintToken(t, key, testKID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if !*called {
		t.Fatal("a valid token must reach the wrapped handler")
	}
	// The middleware delegates to GatewayMiddleware, so the ledger override
	// applies here too while the real identity claims survive.
	if seen.UserID != shared.FinanceLedgerID {
		t.Fatalf("UserID = %q, want the shared ledger %q", seen.UserID, shared.FinanceLedgerID)
	}
	if seen.Subject != "cognito-sub-1" || seen.Email != "ana@loja.com" || seen.Name != "Ana" {
		t.Fatalf("claims = %+v, want the token's sub/email/name", *seen)
	}
	if seen.Phone != "+5511987654321" {
		t.Fatalf("Phone = %q, want the token's phone_number", seen.Phone)
	}
}

func TestLocalCognitoRejectsBadTokens(t *testing.T) {
	key := newKey(t)
	foreign := newKey(t) // a key the JWKS endpoint does not publish

	cases := []struct {
		name          string
		authorization func(t *testing.T) string
		wantError     string
	}{
		{
			"no authorization header",
			func(*testing.T) string { return "" },
			"missing or invalid authorization header",
		},
		{
			"not a bearer scheme",
			func(*testing.T) string { return "Basic dXNlcjpwYXNz" },
			"missing or invalid authorization header",
		},
		{
			"garbage token",
			func(*testing.T) string { return "Bearer not-a-jwt" },
			"invalid token",
		},
		{
			// Signed with a key the JWKS never published: proves the signature
			// itself is verified, not merely the presence of a known kid.
			"signed by an unpublished key",
			func(t *testing.T) string { return "Bearer " + mintToken(t, foreign, testKID, nil) },
			"invalid token",
		},
		{
			"expired",
			func(t *testing.T) string {
				return "Bearer " + mintToken(t, key, testKID, jwt.MapClaims{
					"exp": time.Now().Add(-time.Hour).Unix(),
					"iat": time.Now().Add(-2 * time.Hour).Unix(),
				})
			},
			"invalid token",
		},
		{
			"wrong issuer",
			func(t *testing.T) string {
				return "Bearer " + mintToken(t, key, testKID, jwt.MapClaims{"iss": "https://evil.example.com"})
			},
			"invalid token",
		},
		{
			// An access token has no email/phone and is not what this app's
			// identity model is built on.
			"access token instead of an id token",
			func(t *testing.T) string {
				return "Bearer " + mintToken(t, key, testKID, jwt.MapClaims{"token_use": "access"})
			},
			"not an id token",
		},
		{
			"missing token_use",
			func(t *testing.T) string {
				return "Bearer " + mintToken(t, key, testKID, jwt.MapClaims{"token_use": nil})
			},
			"not an id token",
		},
		{
			// A token minted for a different Cognito app client is valid but
			// not for us.
			"audience for another client",
			func(t *testing.T) string {
				return "Bearer " + mintToken(t, key, testKID, jwt.MapClaims{"aud": "some-other-client"})
			},
			"unrecognized audience",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, called := newMiddleware(t, key)
			w := doRequest(h, tc.authorization(t))

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", w.Code, w.Body.String())
			}
			if *called {
				t.Fatal("a rejected token must never reach the wrapped handler")
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", w.Body.String(), err)
			}
			if body["error"] != tc.wantError {
				t.Fatalf("error = %q, want %q", body["error"], tc.wantError)
			}
		})
	}
}

func TestLocalCognitoRejectsAnUnsignedToken(t *testing.T) {
	key := newKey(t)
	h, _, called := newMiddleware(t, key)

	// alg=none is the classic JWT bypass; jwt.WithValidMethods("RS256") is what
	// stops it.
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "attacker", "token_use": "id", "aud": testClientID, "iss": testIssuer,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	unsigned, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}

	w := doRequest(h, "Bearer "+unsigned)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — an alg=none token was accepted", w.Code)
	}
	if *called {
		t.Fatal("an unsigned token must never reach the wrapped handler")
	}
}

func TestLocalCognitoRejectsATokenWithoutASubject(t *testing.T) {
	key := newKey(t)
	h, _, called := newMiddleware(t, key)

	// The token verifies, but carries no identity — GatewayMiddleware is what
	// turns that into a 401.
	w := doRequest(h, "Bearer "+mintToken(t, key, testKID, jwt.MapClaims{"sub": nil}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if *called {
		t.Fatal("a subject-less token must not reach the wrapped handler")
	}
}
