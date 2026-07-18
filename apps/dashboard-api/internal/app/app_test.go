package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"golang.org/x/crypto/bcrypt"

	pkgauth "github.com/emerson/emerbot/packages/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

const (
	testEmail    = "demo@user.com"
	testPassword = "pw123456"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	authStore := pkgauth.NewInMemoryStore()
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := authStore.SaveUser(context.Background(), domain.User{
		UserID: "u1", Email: testEmail, PasswordHash: string(hash), Name: "Demo",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return New(authStore, pkgfinance.NewInMemoryStore(), "test-secret")
}

func do(t *testing.T, app *App, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func login(t *testing.T, app *App) string {
	t.Helper()
	rec := do(t, app, http.MethodPost, "/auth/login", "", map[string]string{
		"email": testEmail, "password": testPassword,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("expected a non-empty access token")
	}
	return resp.AccessToken
}

func TestLogin(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	// Valid credentials -> token (asserted in login()).
	_ = login(t, app)

	// Wrong password -> 401.
	rec := do(t, app, http.MethodPost, "/auth/login", "", map[string]string{
		"email": testEmail, "password": "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", rec.Code)
	}
}

func TestProtectedRouteRequiresAuth(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	if rec := do(t, app, http.MethodGet, "/entries", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
	if rec := do(t, app, http.MethodGet, "/entries", "garbage", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a bad token, got %d", rec.Code)
	}
	if rec := do(t, app, http.MethodGet, "/entries", login(t, app), nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with a valid token, got %d", rec.Code)
	}
}

func TestEntriesCRUD(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	token := login(t, app)

	// Create.
	rec := do(t, app, http.MethodPost, "/entries", token, map[string]any{
		"date": "2026-07-10", "amount": 50000, "category": "aluguel",
		"type": "expense", "description": "Aluguel", "payment_status": "paid",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created domain.FinancialEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created entry: %v", err)
	}
	if created.EntryID == "" {
		t.Fatal("expected a non-empty entry id")
	}

	// List -> count 1.
	if got := listCount(t, app, token); got != 1 {
		t.Fatalf("expected 1 entry after create, got %d", got)
	}

	// Update.
	rec = do(t, app, http.MethodPut, "/entries/"+created.EntryID, token, map[string]any{"amount": 75000})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Delete -> 204, then list is empty.
	if rec := do(t, app, http.MethodDelete, "/entries/"+created.EntryID, token, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
	if got := listCount(t, app, token); got != 0 {
		t.Fatalf("expected 0 entries after delete, got %d", got)
	}
}

func listCount(t *testing.T, app *App, token string) int {
	t.Helper()
	rec := do(t, app, http.MethodGet, "/entries", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return resp.Count
}

func TestSummaryMonthly(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	token := login(t, app)
	if rec := do(t, app, http.MethodGet, "/summary/monthly?month=2026-07", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /summary/monthly, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestGoals(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	token := login(t, app)

	rec := do(t, app, http.MethodPut, "/goals", token, map[string]any{
		"month": "2026-07", "revenue_target": 8000000, "expense_target": 6000000,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save goal: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = do(t, app, http.MethodGet, "/goals?month=2026-07", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get goal: expected 200, got %d", rec.Code)
	}
	var resp struct {
		Goal *domain.Goal `json:"goal"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode goal response: %v", err)
	}
	if resp.Goal == nil || resp.Goal.RevenueTarget != 8000000 {
		t.Fatalf("expected saved goal with revenue target 8000000, got %+v", resp.Goal)
	}
}

func TestCategoriesSeedsDefaults(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	token := login(t, app)

	rec := do(t, app, http.MethodGet, "/categories", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /categories, got %d", rec.Code)
	}
	var resp struct {
		Categories []domain.Category `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode categories response: %v", err)
	}
	if len(resp.Categories) == 0 {
		t.Fatal("expected default categories to be seeded on first call")
	}
}

func TestCORSPreflight(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	rec := do(t, app, http.MethodOptions, "/entries", "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for CORS preflight, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("expected CORS Access-Control-Allow-Origin header")
	}
}

func TestGatewayClaimsBridgeProtectsFinanceRoutes(t *testing.T) {
	t.Parallel()
	app := NewGateway(pkgauth.NewInMemoryStore(), pkgfinance.NewInMemoryStore(), "test-secret")
	event := events.APIGatewayV2HTTPRequest{
		Version: "2.0",
		RawPath: "/entries",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{Method: http.MethodGet, Path: "/entries"},
		},
	}

	withoutClaims, err := app.HandleLambda(context.Background(), event)
	if err != nil {
		t.Fatalf("handle event without claims: %v", err)
	}
	if withoutClaims.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without gateway claims, got %d", withoutClaims.StatusCode)
	}

	event.RequestContext.Authorizer = &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
		JWT: &events.APIGatewayV2HTTPRequestContextAuthorizerJWTDescription{Claims: map[string]string{
			"sub": "cognito-user-id", "email": "demo@user.com", "username": "Demo",
		}},
	}
	withClaims, err := app.HandleLambda(context.Background(), event)
	if err != nil {
		t.Fatalf("handle event with claims: %v", err)
	}
	if withClaims.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with gateway claims, got %d (%s)", withClaims.StatusCode, withClaims.Body)
	}
}

func TestFinanceLedgerSharedAcrossUsers(t *testing.T) {
	t.Parallel()

	authStore := pkgauth.NewInMemoryStore()
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	for _, u := range []domain.User{
		{UserID: "u1", Email: "a@user.com", PasswordHash: string(hash), Name: "A"},
		{UserID: "u2", Email: "b@user.com", PasswordHash: string(hash), Name: "B"},
	} {
		if err := authStore.SaveUser(context.Background(), u); err != nil {
			t.Fatalf("seed user %s: %v", u.Email, err)
		}
	}
	app := New(authStore, pkgfinance.NewInMemoryStore(), "test-secret")

	// User A creates an entry.
	tokenA := loginAs(t, app, "a@user.com", testPassword)
	rec := do(t, app, http.MethodPost, "/entries", tokenA, map[string]any{
		"date": "2026-07-10", "amount": 50000, "category": "aluguel",
		"type": "expense", "description": "Aluguel", "payment_status": "paid",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	// User B — a DIFFERENT account — must see the same shared ledger.
	tokenB := loginAs(t, app, "b@user.com", testPassword)
	if got := listCount(t, app, tokenB); got != 1 {
		t.Fatalf("expected user B to see 1 shared entry, got %d", got)
	}
}

func loginAs(t *testing.T, app *App, email, password string) string {
	t.Helper()
	rec := do(t, app, http.MethodPost, "/auth/login", "", map[string]string{
		"email": email, "password": password,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: expected 200, got %d (%s)", email, rec.Code, rec.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return resp.AccessToken
}
