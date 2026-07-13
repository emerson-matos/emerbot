package app

import (
	"context"
	"net/http"

	"github.com/aws/aws-lambda-go/events"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	apifinance "github.com/emerson/emerbot/apps/dashboard-api/internal/finance"
	pkgauth "github.com/emerson/emerbot/packages/auth"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// App wires all handlers and exposes both a local HTTP server
// and an API Gateway Lambda handler.
type App struct {
	handler http.Handler
}

func New(authStore pkgauth.Store, finStore pkgfinance.Store, jwtSecret string) *App {
	jwt := pkgauth.NewJWT(jwtSecret)
	authMw := apiauth.Middleware(jwt)

	authHandler := apiauth.NewHandler(authStore, jwt)
	entriesHandler := apifinance.NewEntriesHandler(finStore)
	summaryHandler := apifinance.NewSummaryHandler(finStore)
	catsHandler := apifinance.NewCategoriesHandler(finStore)

	mux := http.NewServeMux()

	// Health check.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Auth routes (no middleware).
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.HandleFunc("POST /auth/refresh", authHandler.Refresh)

	// Protected routes — wrapped with JWT middleware.
	mux.Handle("GET /entries", authMw(http.HandlerFunc(entriesHandler.List)))
	mux.Handle("POST /entries", authMw(http.HandlerFunc(entriesHandler.Create)))
	mux.Handle("PUT /entries/{id}", authMw(http.HandlerFunc(entriesHandler.Update)))
	mux.Handle("DELETE /entries/{id}", authMw(http.HandlerFunc(entriesHandler.Delete)))

	mux.Handle("GET /summary/monthly", authMw(http.HandlerFunc(summaryHandler.Monthly)))
	mux.Handle("GET /summary/categories", authMw(http.HandlerFunc(summaryHandler.Categories)))
	mux.Handle("GET /summary/cashflow", authMw(http.HandlerFunc(summaryHandler.CashFlow)))

	goalHandler := apifinance.NewGoalsHandler(finStore)

	mux.Handle("GET /categories", authMw(http.HandlerFunc(catsHandler.List)))
	mux.Handle("POST /categories", authMw(http.HandlerFunc(catsHandler.Create)))

	mux.Handle("GET /goals", authMw(http.HandlerFunc(goalHandler.Get)))
	mux.Handle("PUT /goals", authMw(http.HandlerFunc(goalHandler.Save)))

	return &App{handler: withCORS(mux)}
}

// ServeHTTP satisfies http.Handler — used by the local cmd.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

// HandleLambda adapts the app to API Gateway V2 HTTP Lambda events using
// a simple request/response bridge — avoids the heavy lambda adapter.
func (a *App) HandleLambda(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	rec := &responseRecorder{headers: make(http.Header), statusCode: http.StatusOK}
	req, err := apiGWEventToHTTPRequest(ctx, event)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusBadRequest}, nil
	}
	a.handler.ServeHTTP(rec, req)
	return rec.toAPIGWResponse(), nil
}

// withCORS wraps a handler and injects CORS headers on every response.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

