package auth

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestNewLocalCognitoMiddlewareFailsFastOnUnreachableJWKS(t *testing.T) {
	t.Parallel()

	// A JWKS server we immediately close is a stand-in for "cognito-local isn't
	// up yet" — the constructor must surface that as an error rather than
	// silently building a middleware with an empty key set (which is what
	// keyfunc.NewDefaultCtx would do; see the comment in local_middleware.go
	// on why this uses jwkset.NewStorageFromHTTP directly instead).
	srv := httptest.NewServer(nil)
	unreachableURL := srv.URL
	srv.Close()

	if _, err := NewLocalCognitoMiddleware(context.Background(), unreachableURL, "https://issuer.example.com", "client-id"); err == nil {
		t.Fatal("expected an error building middleware against an unreachable JWKS endpoint, got nil")
	}
}
