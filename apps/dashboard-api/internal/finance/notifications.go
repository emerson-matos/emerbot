package finance

import (
	"context"
	"log"
	"net/http"
	"strings"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/domain"
)

// NotificationPrefsStore is the slice of the finance store the notification
// endpoint uses.
type NotificationPrefsStore interface {
	GetNotificationPrefs(ctx context.Context, userID string) (domain.NotificationPrefs, error)
	SaveNotificationPrefs(ctx context.Context, prefs domain.NotificationPrefs) error
}

type NotificationsHandler struct {
	store NotificationPrefsStore
}

func NewNotificationsHandler(store NotificationPrefsStore) *NotificationsHandler {
	return &NotificationsHandler{store: store}
}

// notifPrefsResponse is what the Notificações page renders. There is nothing to
// choose any more — the messages always go out — so the only thing worth showing
// is where they go, and the only thing that can go wrong is this being empty.
type notifPrefsResponse struct {
	Phone string `json:"phone"`
}

// Get handles GET /notifications/preferences, and registers the caller as a
// recipient on the way.
//
// The registration used to be the PUT: the page saved your switches, and saving
// them is what put your user and phone in the table the notifier reads. With the
// switches gone there is nothing left to save — but the notifier still needs an
// address book, and this is the only moment the system sees a real Cognito user
// and their phone number together.
//
// So the read writes, when and only when the stored row does not already say
// this. That also fixes something the PUT never did: a phone changed on the
// Cognito account used to leave the old number in the table until the user
// happened to re-save the form, which meant the messages kept going to a number
// they had already replaced.
func (h *NotificationsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	prefs := domain.NotificationPrefs{UserID: claims.Subject, Phone: normalizePhone(claims.Phone)}
	stored, err := h.store.GetNotificationPrefs(r.Context(), claims.Subject)
	if err != nil || stored != prefs {
		// A failed read is treated as "not registered yet" rather than an error:
		// the page's job is to say where the messages go, and it can do that from
		// the claims alone. Writing on that path costs one PutItem and makes the
		// registry self-healing.
		if serr := h.store.SaveNotificationPrefs(r.Context(), prefs); serr != nil {
			// Not fatal either. The number is still correct on screen; it is the
			// next run of the notifier that would use a stale one, and that is
			// worth a log rather than an error page.
			log.Printf("register notification recipient: %v", serr)
		}
	}

	httpx.OK(w, map[string]any{"preferences": notifPrefsResponse{Phone: prefs.Phone}})
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
