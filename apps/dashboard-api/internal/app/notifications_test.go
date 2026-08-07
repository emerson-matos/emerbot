package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

type notifPrefsResp struct {
	Preferences struct {
		Phone string `json:"phone"`
	} `json:"preferences"`
}

func decodePrefs(t *testing.T, body []byte) notifPrefsResp {
	t.Helper()
	var resp notifPrefsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prefs: %v", err)
	}
	return resp
}

// TestNotificationDeliveryShowsTheAccountsPhone: with the switches gone the page
// has one thing to say, and it comes from the verified Cognito claim rather than
// from anything the client sends.
func TestNotificationDeliveryShowsTheAccountsPhone(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	token := mintTokenWithOverrides(t, key, testKID, "u1", "demo@user.com", "Demo",
		jwt.MapClaims{"phone_number": "+5511987654321"})

	rec := do(t, app, http.MethodGet, "/notifications/preferences", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodePrefs(t, rec.Body.Bytes()).Preferences.Phone; got != "5511987654321" {
		t.Fatalf("phone = %q, want the claim's digits", got)
	}
}

// TestNotificationDeliveryRegistersTheRecipient is the load-bearing half of that
// endpoint now: it is the only place a real Cognito user and their phone are
// seen together, so reading it is what puts them in the table the notifier sends
// from. Before, that was the PUT the settings form issued — and with no form
// left to submit, nobody would ever be registered.
func TestNotificationDeliveryRegistersTheRecipient(t *testing.T) {
	t.Parallel()
	app, store, key := newTestAppWithStore(t)
	token := mintTokenWithOverrides(t, key, testKID, "u1", "demo@user.com", "Demo",
		jwt.MapClaims{"phone_number": "+5511987654321"})

	if rec := do(t, app, http.MethodGet, "/notifications/preferences", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	prefs, err := store.ListNotificationPrefs(t.Context())
	if err != nil {
		t.Fatalf("list recipients: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("want 1 registered recipient, got %d: %+v", len(prefs), prefs)
	}
	if prefs[0].Phone != "5511987654321" {
		t.Errorf("registered phone = %q, want the claim's digits", prefs[0].Phone)
	}
}

// TestNotificationDeliveryIsPerCognitoUser: two accounts, two recipients. They
// all watch the same shared ledger, but each is messaged on their own number.
func TestNotificationDeliveryIsPerCognitoUser(t *testing.T) {
	t.Parallel()
	app, store, key := newTestAppWithStore(t)
	tokenA := mintTokenWithOverrides(t, key, testKID, "userA", "a@user.com", "A",
		jwt.MapClaims{"sub": "sub-a", "phone_number": "+5511900000001"})
	tokenB := mintTokenWithOverrides(t, key, testKID, "userB", "b@user.com", "B",
		jwt.MapClaims{"sub": "sub-b", "phone_number": "+5511900000002"})

	for _, token := range []string{tokenA, tokenB} {
		if rec := do(t, app, http.MethodGet, "/notifications/preferences", token, nil); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
	}

	prefs, err := store.ListNotificationPrefs(t.Context())
	if err != nil {
		t.Fatalf("list recipients: %v", err)
	}
	phones := make(map[string]bool, len(prefs))
	for _, p := range prefs {
		phones[p.Phone] = true
	}
	if len(prefs) != 2 || !phones["5511900000001"] || !phones["5511900000002"] {
		t.Fatalf("want both accounts registered on their own numbers, got %+v", prefs)
	}
}

// TestNotificationDeliveryWithoutAPhone: an account with no number is not an
// error to look at — the page still renders — but there is nothing to send to,
// and the notifier reports it as unreachable rather than silently skipping.
func TestNotificationDeliveryWithoutAPhone(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")

	rec := do(t, app, http.MethodGet, "/notifications/preferences", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodePrefs(t, rec.Body.Bytes()).Preferences.Phone; got != "" {
		t.Fatalf("phone = %q, want empty", got)
	}
}

// TestNotificationPreferencesRejectsWrites: there is nothing left to configure,
// so the endpoint is read-only. A PUT is Method Not Allowed rather than a silent
// no-op that would let a stale client believe it saved something.
func TestNotificationPreferencesRejectsWrites(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	token := mintTokenWithOverrides(t, key, testKID, "u1", "demo@user.com", "Demo",
		jwt.MapClaims{"phone_number": "+5511987654321"})

	rec := do(t, app, http.MethodPut, "/notifications/preferences", token, map[string]any{"waEnabled": false})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d (%s)", rec.Code, rec.Body.String())
	}
}
