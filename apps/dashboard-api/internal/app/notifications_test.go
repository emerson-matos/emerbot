package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

type notifPrefsResp struct {
	Preferences struct {
		WAEnabled      bool   `json:"waEnabled"`
		Phone          string `json:"phone"`
		NotifyDueToday bool   `json:"notifyDueToday"`
		NotifyOverdue  bool   `json:"notifyOverdue"`
		NotifyGoal     bool   `json:"notifyGoal"`
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

func TestNotificationPrefsDefaultsThenSave(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	token := mintTokenWithOverrides(t, key, testKID, "u1", "demo@user.com", "Demo",
		jwt.MapClaims{"phone_number": "+5511987654321"})

	// GET before saving anything -> opt-in defaults (WhatsApp off), phone
	// already reflects the Cognito account (not yet saved anywhere).
	rec := do(t, app, http.MethodGet, "/notifications/preferences", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get defaults: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	def := decodePrefs(t, rec.Body.Bytes())
	if def.Preferences.WAEnabled {
		t.Fatal("WhatsApp delivery should default off (opt-in)")
	}
	if def.Preferences.Phone != "5511987654321" {
		t.Fatalf("phone should mirror the Cognito claim, got %q", def.Preferences.Phone)
	}
	if !def.Preferences.NotifyDueToday || !def.Preferences.NotifyOverdue {
		t.Fatal("due-today and overdue alert types should default on")
	}

	// PUT ignores any client-supplied phone and stores the Cognito one instead.
	rec = do(t, app, http.MethodPut, "/notifications/preferences", token, map[string]any{
		"waEnabled": true, "phone": "5599999999999", "notifyGoal": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save prefs: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	saved := decodePrefs(t, rec.Body.Bytes())
	if saved.Preferences.Phone != "5511987654321" {
		t.Fatalf("phone should come from the Cognito claim, got %q", saved.Preferences.Phone)
	}
	if !saved.Preferences.WAEnabled || !saved.Preferences.NotifyGoal {
		t.Fatalf("saved prefs not applied: %+v", saved.Preferences)
	}

	// GET returns what was stored (and a partial PUT preserved the defaulted
	// due-today flag).
	rec = do(t, app, http.MethodGet, "/notifications/preferences", token, nil)
	got := decodePrefs(t, rec.Body.Bytes())
	if got.Preferences.Phone != "5511987654321" || !got.Preferences.NotifyDueToday {
		t.Fatalf("get after save: %+v", got.Preferences)
	}
}

func TestNotificationPrefsEnableRequiresPhone(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	// No phone_number claim on this account.
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")

	rec := do(t, app, http.MethodPut, "/notifications/preferences", token, map[string]any{
		"waEnabled": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("enabling with no phone should be 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestNotificationPrefsAreIsolatedPerCognitoUser is the regression test for
// the identity-collapsing bug: two different Cognito users (distinct subs)
// must each get their own preferences — one saving must not clobber or leak
// into the other's.
func TestNotificationPrefsAreIsolatedPerCognitoUser(t *testing.T) {
	t.Parallel()
	app, key := newTestApp(t)
	tokenA := mintTokenWithOverrides(t, key, testKID, "u1", "a@user.com", "A",
		jwt.MapClaims{"phone_number": "+5511900000001"})
	tokenB := mintTokenWithOverrides(t, key, testKID, "u2", "b@user.com", "B",
		jwt.MapClaims{"phone_number": "+5511900000002"})

	rec := do(t, app, http.MethodPut, "/notifications/preferences", tokenA, map[string]any{
		"waEnabled": true, "notifyGoal": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save A: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = do(t, app, http.MethodGet, "/notifications/preferences", tokenB, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get B: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	b := decodePrefs(t, rec.Body.Bytes())
	if b.Preferences.WAEnabled || b.Preferences.NotifyGoal {
		t.Fatalf("user B must not see user A's saved prefs, got %+v", b.Preferences)
	}
	if b.Preferences.Phone != "5511900000002" {
		t.Fatalf("user B's phone must be their own Cognito number, got %q", b.Preferences.Phone)
	}

	rec = do(t, app, http.MethodGet, "/notifications/preferences", tokenA, nil)
	a := decodePrefs(t, rec.Body.Bytes())
	if !a.Preferences.WAEnabled || !a.Preferences.NotifyGoal || a.Preferences.Phone != "5511900000001" {
		t.Fatalf("user A's own prefs should be unaffected, got %+v", a.Preferences)
	}
}

func TestNotificationPrefsRequireAuth(t *testing.T) {
	t.Parallel()
	app, _ := newTestApp(t)
	if rec := do(t, app, http.MethodGet, "/notifications/preferences", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
