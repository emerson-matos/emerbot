package domain

// NotificationPrefs holds a user's WhatsApp alert preferences (the settings
// managed on the dashboard's Notificações page). Phone is stored as E.164
// digits — country code + number, no leading "+" — which is what the Meta
// Cloud API's `to` field expects.
type NotificationPrefs struct {
	UserID         string
	WAEnabled      bool
	Phone          string
	NotifyDueToday bool
	NotifyOverdue  bool
	NotifyGoal     bool
}

// DefaultNotificationPrefs is what a user gets before they save anything.
// WhatsApp delivery is opt-in (off), but the alert *types* default on so that
// flipping the switch is immediately useful.
func DefaultNotificationPrefs(userID string) NotificationPrefs {
	return NotificationPrefs{
		UserID:         userID,
		WAEnabled:      false,
		Phone:          "",
		NotifyDueToday: true,
		NotifyOverdue:  true,
		NotifyGoal:     false,
	}
}
