package app

import (
	"encoding/json"
	"net/http"
	"testing"
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
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")

	// GET before saving anything -> opt-in defaults (WhatsApp off).
	rec := do(t, app, http.MethodGet, "/notifications/preferences", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get defaults: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	def := decodePrefs(t, rec.Body.Bytes())
	if def.Preferences.WAEnabled {
		t.Fatal("WhatsApp delivery should default off (opt-in)")
	}
	if !def.Preferences.NotifyDueToday || !def.Preferences.NotifyOverdue {
		t.Fatal("due-today and overdue alert types should default on")
	}

	// PUT normalizes the phone to E.164 and persists.
	rec = do(t, app, http.MethodPut, "/notifications/preferences", token, map[string]any{
		"waEnabled": true, "phone": "(11) 98765-4321", "notifyGoal": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save prefs: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	saved := decodePrefs(t, rec.Body.Bytes())
	if saved.Preferences.Phone != "5511987654321" {
		t.Fatalf("phone should be normalized to E.164, got %q", saved.Preferences.Phone)
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
	token := mintToken(t, key, testKID, "u1", "demo@user.com", "Demo")

	rec := do(t, app, http.MethodPut, "/notifications/preferences", token, map[string]any{
		"waEnabled": true, "phone": "",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("enabling with no phone should be 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestNotificationPrefsRequireAuth(t *testing.T) {
	t.Parallel()
	app, _ := newTestApp(t)
	if rec := do(t, app, http.MethodGet, "/notifications/preferences", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
