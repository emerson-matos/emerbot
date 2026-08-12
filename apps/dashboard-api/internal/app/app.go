package app

import (
	"context"
	"net/http"

	"github.com/aws/aws-lambda-go/events"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	apifiado "github.com/emerson/emerbot/apps/dashboard-api/internal/fiado"
	apifinance "github.com/emerson/emerbot/apps/dashboard-api/internal/finance"
	apipayments "github.com/emerson/emerbot/apps/dashboard-api/internal/payments"
	pkgfiado "github.com/emerson/emerbot/packages/fiado"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	pkgpayments "github.com/emerson/emerbot/packages/payments"
	"github.com/emerson/emerbot/packages/shared"
)

// App wires all handlers and exposes both a local HTTP server
// and an API Gateway Lambda handler.
type App struct {
	handler http.Handler
}

// NewGateway is used by the deployed Lambda behind API Gateway's Cognito JWT
// authorizer, which has already validated the request's JWT before Lambda
// runs — see bridge.go's gatewayClaims. This still adds its own CORS headers
// via withCORS (needed for the OPTIONS preflight route, which API Gateway
// forwards straight to the Lambda — see the comment on
// dashboard_public_routes in infra/modules/api_gateway_lambda/main.tf) even
// though API Gateway's own cors_configuration also covers this API: once
// cors_configuration is set, API Gateway overrides whatever CORS headers the
// backend integration returns with its own, so there's no duplication —
// cors_configuration's real job is attaching CORS headers to responses API
// Gateway generates itself, like a 401/403 from the JWT authorizer rejecting
// a request before it ever reaches this Lambda.
func NewGateway(finStore pkgfinance.Store, payRepo pkgpayments.Repository, fiadoStore pkgfiado.Store) *App {
	return newApp(finStore, payRepo, fiadoStore, apiauth.GatewayMiddleware)
}

// NewLocal is used by cmd/local, which has no API Gateway in front of it — it
// verifies Cognito JWTs itself via JWKS (see apiauth.NewLocalCognitoMiddleware)
// instead of trusting pre-validated claims.
func NewLocal(finStore pkgfinance.Store, payRepo pkgpayments.Repository, fiadoStore pkgfiado.Store, authMw func(http.Handler) http.Handler) *App {
	return newApp(finStore, payRepo, fiadoStore, authMw)
}

// newApp wires the routes shared by both entrypoints. NOTE: this route list
// must stay in sync with the dashboard_protected_routes/dashboard_public_routes
// locals in infra/modules/api_gateway_lambda/main.tf — there is no
// compile-time link between the two.
func newApp(finStore pkgfinance.Store, payRepo pkgpayments.Repository, fiadoStore pkgfiado.Store, authMw func(http.Handler) http.Handler) *App {
	// "Hoje", "esta semana" and "dias restantes" are the pharmacy's calendar
	// day, not the Lambda's UTC one — every handler below that answers a
	// "current month"/"today" question shares this one location.
	pharmacyLoc := shared.PharmacyLocation()

	entriesHandler := apifinance.NewEntriesHandler(finStore, pharmacyLoc)
	summaryHandler := apifinance.NewSummaryHandler(finStore, pharmacyLoc)
	catsHandler := apifinance.NewCategoriesHandler(finStore)
	paymentsHandler := apipayments.NewHandler(payRepo, finStore, pharmacyLoc)
	analysisHandler := apifinance.NewAnalysisHandler(finStore, pharmacyLoc)

	mux := http.NewServeMux()

	// Health check.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Protected routes — wrapped with JWT middleware.
	mux.Handle("GET /entries", authMw(http.HandlerFunc(entriesHandler.List)))
	mux.Handle("POST /entries", authMw(http.HandlerFunc(entriesHandler.Create)))
	// One entry is addressed by date *and* id: the two together are its row key
	// (see apifinance.entryAddress). Every route added here must also be added
	// to dashboard_protected_routes in infra/modules/api_gateway_lambda —
	// the gateway enumerates its routes, so a handler with no route there is a
	// 404 that never reaches this mux.
	mux.Handle("GET /entries/{date}/{id}", authMw(http.HandlerFunc(entriesHandler.Get)))
	mux.Handle("PUT /entries/{date}/{id}", authMw(http.HandlerFunc(entriesHandler.Update)))
	mux.Handle("DELETE /entries/{date}/{id}", authMw(http.HandlerFunc(entriesHandler.Delete)))

	mux.Handle("GET /summary/monthly", authMw(http.HandlerFunc(summaryHandler.Monthly)))
	mux.Handle("GET /summary/categories", authMw(http.HandlerFunc(summaryHandler.Categories)))
	mux.Handle("GET /summary/cashflow", authMw(http.HandlerFunc(summaryHandler.CashFlow)))

	// The whole analysis in one call — health, trends, weekday averages, week
	// comparison, goal pace, cash position and recommendations.
	mux.Handle("GET /analysis/monthly", authMw(http.HandlerFunc(analysisHandler.Monthly)))
	// ADR-030: an observation-only forecast candidate, separate from the
	// operational analysis and its stored daily snapshots.
	mux.Handle("GET /analysis/projection-experiment", authMw(http.HandlerFunc(analysisHandler.ProjectionExperiment)))

	// Cached daily snapshot — the notifier persists it, this serves it.
	// POST /analysis triggers an on-demand recalculation.
	snapshotHandler := apifinance.NewSnapshotHandler(finStore, pharmacyLoc)
	mux.Handle("GET /analysis", authMw(http.HandlerFunc(snapshotHandler.Get)))
	mux.Handle("POST /analysis", authMw(http.HandlerFunc(snapshotHandler.Recalculate)))

	goalHandler := apifinance.NewGoalsHandler(finStore, pharmacyLoc)

	mux.Handle("GET /categories", authMw(http.HandlerFunc(catsHandler.List)))
	mux.Handle("POST /categories", authMw(http.HandlerFunc(catsHandler.Create)))

	mux.Handle("GET /goals", authMw(http.HandlerFunc(goalHandler.Get)))
	mux.Handle("PUT /goals", authMw(http.HandlerFunc(goalHandler.Save)))

	notifHandler := apifinance.NewNotificationsHandler(finStore)

	mux.Handle("GET /notifications/preferences", authMw(http.HandlerFunc(notifHandler.Get)))

	// The caderninho de fiado (ADR-027), read-only: registering a purchase and
	// settling one happen in the WhatsApp conversation, where the client's name
	// is reconciled before anything is written.
	//
	// Wired only when there is a store for it, so an entrypoint that has not
	// built one answers 404 rather than panicking on the first request.
	if fiadoStore != nil {
		fiadoHandler := apifiado.NewHandler(fiadoStore, pharmacyLoc)
		mux.Handle("GET /fiado", authMw(http.HandlerFunc(fiadoHandler.List)))
		// "GET /fiado/movimentos" is registered too, and Go's mux prefers the
		// literal segment over the wildcard, so it wins over /fiado/{cliente}.
		// The cost is that a client whose slug is literally "movimentos" is
		// unreachable, which is a trade nobody will ever notice.
		mux.Handle("GET /fiado/movimentos", authMw(http.HandlerFunc(fiadoHandler.DayMovements)))
		mux.Handle("GET /fiado/{cliente}", authMw(http.HandlerFunc(fiadoHandler.Get)))
		mux.Handle("GET /fiado/{cliente}/movimentos", authMw(http.HandlerFunc(fiadoHandler.ClientMovements)))
	}

	// Imported payment-processor data (read-only; writes go through the
	// payment-importer Lambda triggered by S3).
	mux.Handle("GET /payments/sales", authMw(http.HandlerFunc(paymentsHandler.Sales)))
	mux.Handle("GET /payments/receivables", authMw(http.HandlerFunc(paymentsHandler.Receivables)))
	mux.Handle("GET /payments/forecast", authMw(http.HandlerFunc(paymentsHandler.Forecast)))

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
