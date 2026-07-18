package auth

import (
	"context"
	"net/http"
	"strings"

	pkgauth "github.com/emerson/emerbot/packages/auth"
	"github.com/emerson/emerbot/packages/shared"
)

type contextKey string

const claimsKey contextKey = "claims"

// WithClaims attaches trusted authentication claims to a request context.
func WithClaims(ctx context.Context, claims pkgauth.Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// Middleware validates the Authorization: Bearer <token> header.
// On success it injects the claims into the request context.
func Middleware(jwt *pkgauth.JWT) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				jsonError(w, "missing or invalid authorization header", http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")
			claims, err := jwt.Verify(tokenStr)
			if err != nil {
				jsonError(w, "invalid token", http.StatusUnauthorized)
				return
			}
			// TODO(mock): all authenticated users share one finance ledger until
			// phone→account linking exists. Override the storage identity only;
			// claims.Email/Name stay real.
			claims.UserID = shared.FinanceLedgerID
			ctx := WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GatewayMiddleware accepts only claims API Gateway has validated with the
// configured Cognito JWT authorizer.
func GatewayMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || claims.UserID == "" {
			jsonError(w, "missing authenticated identity", http.StatusUnauthorized)
			return
		}
		claims.UserID = shared.FinanceLedgerID
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}

// ClaimsFromContext extracts the authenticated claims from the request context.
func ClaimsFromContext(ctx context.Context) (pkgauth.Claims, bool) {
	c, ok := ctx.Value(claimsKey).(pkgauth.Claims)
	return c, ok
}
