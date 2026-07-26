package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emerson/emerbot/packages/shared"
)

// nextRecorder captures the claims a middleware hands downstream, which is the
// only externally visible result of GatewayMiddleware's work.
func nextRecorder(seen *Claims, called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		if c, ok := ClaimsFromContext(r.Context()); ok {
			*seen = c
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestClaimsRoundTripThroughContext(t *testing.T) {
	want := Claims{UserID: "u1", Email: "a@b.com", Name: "Ana", Phone: "+5511999", Subject: "sub-1"}
	ctx := WithClaims(context.Background(), want)

	got, ok := ClaimsFromContext(ctx)
	if !ok {
		t.Fatal("expected claims to be present in the context")
	}
	if got != want {
		t.Fatalf("claims = %+v, want %+v", got, want)
	}
}

func TestClaimsFromContextIsAbsentOnABareContext(t *testing.T) {
	if _, ok := ClaimsFromContext(context.Background()); ok {
		t.Fatal("a context with no claims must not report any")
	}
}

func TestGatewayMiddlewareRejectsUnauthenticatedRequests(t *testing.T) {
	cases := map[string]context.Context{
		"no claims at all": context.Background(),
		// An established-but-anonymous identity is not an identity: without a
		// subject there is nobody to attribute the request to.
		"claims with an empty subject": WithClaims(context.Background(), Claims{UserID: "u1"}),
	}

	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			var seen Claims
			called := false
			h := GatewayMiddleware(nextRecorder(&seen, &called))

			r := httptest.NewRequest(http.MethodGet, "/entries", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if called {
				t.Fatal("the wrapped handler must not run for an unauthenticated request")
			}
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", w.Body.String(), err)
			}
			if body["error"] != "missing authenticated identity" {
				t.Fatalf("error = %q, want \"missing authenticated identity\"", body["error"])
			}
		})
	}
}

func TestGatewayMiddlewarePassesAuthenticatedRequestsThrough(t *testing.T) {
	var seen Claims
	called := false
	h := GatewayMiddleware(nextRecorder(&seen, &called))

	incoming := Claims{UserID: "cognito-sub", Email: "ana@loja.com", Name: "Ana", Phone: "+5511999", Subject: "cognito-sub"}
	r := httptest.NewRequest(http.MethodGet, "/entries", nil).WithContext(WithClaims(context.Background(), incoming))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if !called {
		t.Fatal("an authenticated request must reach the wrapped handler")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Only the storage identity is overridden onto the shared ledger; the real
	// identity claims must pass through untouched.
	if seen.UserID != shared.FinanceLedgerID {
		t.Fatalf("UserID = %q, want it overridden to %q", seen.UserID, shared.FinanceLedgerID)
	}
	if seen.Subject != incoming.Subject {
		t.Fatalf("Subject = %q, want the real %q — it identifies who wants alerts", seen.Subject, incoming.Subject)
	}
	if seen.Email != incoming.Email || seen.Name != incoming.Name || seen.Phone != incoming.Phone {
		t.Fatalf("claims = %+v, want email/name/phone preserved from %+v", seen, incoming)
	}
}

func TestGatewayMiddlewareDoesNotMutateTheCallersClaims(t *testing.T) {
	incoming := Claims{UserID: "cognito-sub", Subject: "cognito-sub"}
	ctx := WithClaims(context.Background(), incoming)

	var seen Claims
	called := false
	h := GatewayMiddleware(nextRecorder(&seen, &called))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/entries", nil).WithContext(ctx))

	// The ledger override must be scoped to the downstream request; the
	// original context still carries the caller's own user id.
	original, _ := ClaimsFromContext(ctx)
	if original.UserID != "cognito-sub" {
		t.Fatalf("the upstream context's UserID became %q — the override leaked backwards", original.UserID)
	}
}
