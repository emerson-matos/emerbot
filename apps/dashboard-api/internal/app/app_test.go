package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/golang-jwt/jwt/v5"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

const (
	testIssuer   = "https://test-issuer.example.com"
	testClientID = "test-client-id"
	testKID      = "test-kid"
)

// newTestJWKSServer serves a JWKS containing the public half of key, under
// testKID, so NewLocalCognitoMiddleware can verify tokens minted by mintToken.
func newTestJWKSServer(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
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

// mintToken signs a Cognito-shaped ID token. kid lets tests build tokens
// that reference a key the JWKS server doesn't actually hold (signed with an
// unrelated private key), to prove signature verification — not just kid
// presence — is what rejects foreign tokens.
func mintToken(t *testing.T, key *rsa.PrivateKey, kid, sub, email, name string) string {
	t.Helper()
	return mintTokenWithOverrides(t, key, kid, sub, email, name, nil)
}

// mintTokenWithOverrides lets tests replace specific claims (e.g. aud,
// token_use) to exercise NewLocalCognitoMiddleware's checks beyond signature
// verification.
func mintTokenWithOverrides(t *testing.T, key *rsa.PrivateKey, kid, sub, email, name string, overrides jwt.MapClaims) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": sub, "email": email, "name": name,
		"aud": testClientID, "token_use": "id",
		"iss": testIssuer,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	for k, v := range overrides {
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

// newTestApp builds a NewLocal app wired to a local JWKS server, returning the
// private key so tests can mint tokens for it via mintToken.
func newTestApp(t *testing.T) (*App, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	jwksSrv := newTestJWKSServer(t, key)
	authMw, err := apiauth.NewLocalCognitoMiddleware(context.Background(), jwksSrv.URL, testIssuer, testClientID)
	if err != nil {
		t.Fatalf("build local cognito middleware: %v", err)
	}
	return NewLocal(pkgfinance.NewInMemoryStore(), authMw), key
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

func TestProtectedRouteRequiresAuth(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)

	if rec := do(t, app, http.MethodGet, "/entries", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
	if rec := do(t, app, http.MethodGet, "/entries", "garbage", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a bad token, got %d", rec.Code)
	}

	// Well-formed RS256 token referencing a real kid but signed by an
	// unrelated key must also be rejected — proves signature verification
	// actually happens, not just kid-presence matching.
	foreignKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate foreign key: %v", err)
	}
	foreignToken := mintToken(t, foreignKey, testKID, "u1", "demo@user.com", "Demo")
	if rec := do(t, app, http.MethodGet, "/entries", foreignToken, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a foreign-key-signed token, got %d", rec.Code)
	}

	// An access token (token_use "access") must be rejected — only ID tokens
	// carry the profile claims (email/phone_number/name) this app needs, and
	// only ID tokens carry a standard `aud` claim, which is the check API
	// Gateway's real JWT authorizer performs in the deployed path.
	accessToken := mintTokenWithOverrides(t, key, testKID, "u1", "demo@user.com", "Demo", jwt.MapClaims{"token_use": "access"})
	if rec := do(t, app, http.MethodGet, "/entries", accessToken, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with an access token (token_use != id), got %d", rec.Code)
	}

	// A token for a different Cognito app client must be rejected.
	wrongClient := mintTokenWithOverrides(t, key, testKID, "u1", "demo@user.com", "Demo", jwt.MapClaims{"aud": "some-other-client-id"})
	if rec := do(t, app, http.MethodGet, "/entries", wrongClient, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a foreign aud, got %d", rec.Code)
	}

	valid := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")
	if rec := do(t, app, http.MethodGet, "/entries", valid, nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with a valid token, got %d", rec.Code)
	}
}

func TestEntriesCRUD(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")

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
	rec = do(t, app, http.MethodPut, "/entries/"+string(created.EntryID), token, map[string]any{"amount": 75000})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Delete -> 204, then list is empty.
	if rec := do(t, app, http.MethodDelete, "/entries/"+string(created.EntryID), token, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
	if got := listCount(t, app, token); got != 0 {
		t.Fatalf("expected 0 entries after delete, got %d", got)
	}
}

func TestEntriesListLimit(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")

	for _, date := range []string{"2026-07-01", "2026-07-02", "2026-07-03"} {
		rec := do(t, app, http.MethodPost, "/entries", token, map[string]any{
			"date": date, "amount": 1000, "category": "aluguel",
			"type": "expense", "description": "Aluguel " + date, "payment_status": "paid",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create entry %s: expected 201, got %d (%s)", date, rec.Code, rec.Body.String())
		}
	}

	// ?limit=2 caps the response even though 3 entries exist.
	rec := do(t, app, http.MethodGet, "/entries?limit=2", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var resp struct {
		Entries []domain.FinancialEntry `json:"entries"`
		Count   int                     `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if resp.Count != 2 {
		t.Fatalf("expected limit=2 to cap the response at 2 entries, got %d", resp.Count)
	}
	if resp.Entries[0].TransactionDate.Format("2006-01-02") != "2026-07-03" {
		t.Fatalf("expected the most recent entry first, got %s", resp.Entries[0].TransactionDate.Format("2006-01-02"))
	}

	// No limit param -> the server-side default still applies (well above 3,
	// so all 3 come back) rather than the request being rejected or ignored.
	if got := listCount(t, app, token); got != 3 {
		t.Fatalf("expected all 3 entries under the default limit, got %d", got)
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
	app, key := newTestApp(t)
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")
	if rec := do(t, app, http.MethodGet, "/summary/monthly?month=2026-07", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /summary/monthly, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestGoals(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")

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
	app, key := newTestApp(t)
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")

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
	app, _ := newTestApp(t)
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
	app := NewGateway(pkgfinance.NewInMemoryStore())
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
			"sub": "cognito-user-id", "email": "demo@user.com", "name": "Demo",
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
	app, key := newTestApp(t)

	// User A creates an entry.
	tokenA := mintToken(t, key, testKID, "u1", "a@user.com", "A")
	rec := do(t, app, http.MethodPost, "/entries", tokenA, map[string]any{
		"date": "2026-07-10", "amount": 50000, "category": "aluguel",
		"type": "expense", "description": "Aluguel", "payment_status": "paid",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	// User B — a DIFFERENT account — must see the same shared ledger.
	tokenB := mintToken(t, key, testKID, "u2", "b@user.com", "B")
	if got := listCount(t, app, tokenB); got != 1 {
		t.Fatalf("expected user B to see 1 shared entry, got %d", got)
	}
}
