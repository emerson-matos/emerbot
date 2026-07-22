package auth

import (
	"context"
	"net/http"

	"github.com/emerson/emerbot/packages/shared"
)

type contextKey string

const claimsKey contextKey = "claims"

// WithClaims attaches trusted authentication claims to a request context.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// GatewayMiddleware accepts only claims that have already been established as
// trusted for this request — either by API Gateway's Cognito JWT authorizer
// (see bridge.go's gatewayClaims, used by the deployed Lambda) or by
// NewLocalCognitoMiddleware's own JWKS verification (used by cmd/local, which
// has no API Gateway in front of it).
func GatewayMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || claims.Subject == "" {
			jsonError(w, "missing authenticated identity", http.StatusUnauthorized)
			return
		}
		// TODO(mock): all authenticated users share one finance ledger until
		// real per-user financial data exists. Override the storage identity
		// only; claims.Email/Name/Phone/Subject stay real — Subject in
		// particular is what identifies *who* wants WhatsApp alerts, as opposed
		// to *whose* finance ledger this request reads/writes.
		claims.UserID = shared.FinanceLedgerID
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}

// ClaimsFromContext extracts the authenticated claims from the request context.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}
