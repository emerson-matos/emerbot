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
		WAEnabled      *bool   `json:"waEnabled"`
		Phone          *string `json:"phone"`
		NotifyDueToday *bool   `json:"notifyDueToday"`
		NotifyOverdue  *bool   `json:"notifyOverdue"`
		NotifyGoal     *bool   `json:"notifyGoal"`
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
	if body.Phone != nil {
		prefs.Phone = normalizePhoneBR(*body.Phone)
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

	// Can't enable WhatsApp delivery without a phone number to deliver to.
	if prefs.WAEnabled && prefs.Phone == "" {
		jsonError(w, "informe um número de WhatsApp para ativar os alertas", http.StatusBadRequest)
		return
	}

	if err := h.store.SaveNotificationPrefs(r.Context(), prefs); err != nil {
		log.Printf("save notif prefs error: %v", err)
		jsonError(w, "failed to save preferences", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{"preferences": toResponse(prefs)})
}

// normalizePhoneBR reduces a Brazilian phone to E.164 digits (country code +
// number, no "+"). It strips formatting, then ensures the "55" country code:
// 10–11 digit inputs (DDD + number) get "55" prepended; inputs that already
// carry it are left as-is. Anything that doesn't look like a BR number is
// returned digits-only, unchanged otherwise.
func normalizePhoneBR(raw string) string {
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	if d == "" {
		return ""
	}
	// Already prefixed with the 55 country code (12–13 digits total).
	if strings.HasPrefix(d, "55") && (len(d) == 12 || len(d) == 13) {
		return d
	}
	// Bare DDD + number.
	if len(d) == 10 || len(d) == 11 {
		return "55" + d
	}
	return d
}
