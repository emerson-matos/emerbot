package finance

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

type NotificationsHandler struct {
	store pkgfinance.Store
}

func NewNotificationsHandler(store pkgfinance.Store) *NotificationsHandler {
	return &NotificationsHandler{store: store}
}

// notifPrefsResponse is the JSON shape shared by GET and PUT. Phone is echoed
// back normalized so the client can display exactly what was stored.
type notifPrefsResponse struct {
	WAEnabled      bool   `json:"waEnabled"`
	Phone          string `json:"phone"`
	NotifyDueToday bool   `json:"notifyDueToday"`
	NotifyOverdue  bool   `json:"notifyOverdue"`
	NotifyGoal     bool   `json:"notifyGoal"`
}

func toResponse(p domain.NotificationPrefs) notifPrefsResponse {
	return notifPrefsResponse{
		WAEnabled:      p.WAEnabled,
		Phone:          p.Phone,
		NotifyDueToday: p.NotifyDueToday,
		NotifyOverdue:  p.NotifyOverdue,
		NotifyGoal:     p.NotifyGoal,
	}
}

// Get handles GET /notifications/preferences
func (h *NotificationsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	prefs, err := h.store.GetNotificationPrefs(r.Context(), claims.UserID)
	if err != nil {
		// No saved prefs yet — hand back the defaults rather than an error so
		// the form has something to render.
		prefs = domain.DefaultNotificationPrefs(claims.UserID)
	}
	prefs.Phone = normalizePhone(claims.Phone)
	jsonOK(w, map[string]any{"preferences": toResponse(prefs)})
}

// Save handles PUT /notifications/preferences
func (h *NotificationsHandler) Save(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		WAEnabled      *bool `json:"waEnabled"`
		NotifyDueToday *bool `json:"notifyDueToday"`
		NotifyOverdue  *bool `json:"notifyOverdue"`
		NotifyGoal     *bool `json:"notifyGoal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Start from what's stored (or defaults) so a partial PUT only changes the
	// fields it sends.
	prefs, err := h.store.GetNotificationPrefs(r.Context(), claims.UserID)
	if err != nil {
		prefs = domain.DefaultNotificationPrefs(claims.UserID)
	}

	if body.WAEnabled != nil {
		prefs.WAEnabled = *body.WAEnabled
	}
	if body.NotifyDueToday != nil {
		prefs.NotifyDueToday = *body.NotifyDueToday
	}
	if body.NotifyOverdue != nil {
		prefs.NotifyOverdue = *body.NotifyOverdue
	}
	if body.NotifyGoal != nil {
		prefs.NotifyGoal = *body.NotifyGoal
	}

	// The phone is never client-supplied — it's always the Cognito account's
	// registered number, so alerts can't be redirected to an arbitrary number.
	prefs.Phone = normalizePhone(claims.Phone)

	// Can't enable WhatsApp delivery without a phone number to deliver to.
	if prefs.WAEnabled && prefs.Phone == "" {
		jsonError(w, "cadastre um número de telefone na sua conta para ativar os alertas", http.StatusBadRequest)
		return
	}

	if err := h.store.SaveNotificationPrefs(r.Context(), prefs); err != nil {
		log.Printf("save notif prefs error: %v", err)
		jsonError(w, "failed to save preferences", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{"preferences": toResponse(prefs)})
}

// normalizePhone reduces a Cognito phone_number attribute (E.164, e.g.
// "+5511987654321") to bare digits — what the Meta Cloud API's `to` field
// expects.
func normalizePhone(raw string) string {
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	return digits.String()
}
