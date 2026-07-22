package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// NewLocalCognitoMiddleware verifies Cognito-issued ID tokens directly
// against a JWKS endpoint. It exists because cmd/local has no API Gateway JWT
// authorizer in front of it — unlike the deployed Lambda (see bridge.go's
// gatewayClaims, used via GatewayMiddleware), which trusts claims API Gateway
// already validated. This middleware does that same validation itself, then
// delegates to GatewayMiddleware for the identity check and the
// shared.FinanceLedgerID override, so that logic lives in exactly one place.
//
// ID tokens, not access tokens: this app has no OAuth scope/resource-server
// model, it only needs to know who the user is — email/phone_number/name are
// standard claims on an ID token but are never present on a Cognito access
// token, which this app used to (incorrectly) require.
func NewLocalCognitoMiddleware(ctx context.Context, jwksURL, issuer, clientID string) (func(http.Handler) http.Handler, error) {
	// keyfunc.NewDefaultCtx swallows a failing first fetch (jwkset's
	// NoErrorReturnFirstHTTPReq default), which would defeat failing fast here
	// — build the storage directly instead, with that behavior turned off, so
	// an unreachable JWKS endpoint at startup is a real, surfaced error.
	storage, err := jwkset.NewStorageFromHTTP(jwksURL, jwkset.HTTPClientStorageOptions{
		Ctx:             ctx,
		HTTPTimeout:     10 * time.Second,
		RefreshInterval: time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS from %s: %w", jwksURL, err)
	}
	kf, err := keyfunc.New(keyfunc.Options{Ctx: ctx, Storage: storage})
	if err != nil {
		return nil, fmt.Errorf("building keyfunc from JWKS storage: %w", err)
	}

	return func(next http.Handler) http.Handler {
		gw := GatewayMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				jsonError(w, "missing or invalid authorization header", http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")

			token, err := jwt.Parse(
				tokenStr, kf.Keyfunc,
				jwt.WithValidMethods([]string{"RS256"}),
				jwt.WithIssuer(issuer),
			)
			if err != nil || !token.Valid {
				jsonError(w, "invalid token", http.StatusUnauthorized)
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				jsonError(w, "invalid token claims", http.StatusUnauthorized)
				return
			}

			// ID tokens carry a standard `aud` claim (unlike access tokens, which
			// only have client_id) — this mirrors what API Gateway's JWT authorizer
			// validates its `audience` config against in the deployed path (see
			// infra/modules/api_gateway_lambda/main.tf).
			if tokenUse, _ := claims["token_use"].(string); tokenUse != "id" {
				jsonError(w, "not an id token", http.StatusUnauthorized)
				return
			}
			if aud, _ := claims["aud"].(string); aud != clientID {
				jsonError(w, "unrecognized audience", http.StatusUnauthorized)
				return
			}

			sub, _ := claims["sub"].(string)
			// ID tokens carry the real `name` attribute (when set), not
			// `username` — Cognito puts the login name under `cognito:username`
			// instead, which isn't what Claims.Name is meant to represent.
			name, _ := claims["name"].(string)
			email, _ := claims["email"].(string)
			phone, _ := claims["phone_number"].(string)

			reqCtx := WithClaims(r.Context(), Claims{UserID: sub, Email: email, Name: name, Phone: phone, Subject: sub})
			gw.ServeHTTP(w, r.WithContext(reqCtx))
		})
	}, nil
}
