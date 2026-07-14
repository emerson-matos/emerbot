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
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext extracts the authenticated claims from the request context.
func ClaimsFromContext(ctx context.Context) (pkgauth.Claims, bool) {
	c, ok := ctx.Value(claimsKey).(pkgauth.Claims)
	return c, ok
}
