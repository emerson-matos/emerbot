package notifier

import (
	"context"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

type fakeWA struct {
	sent []struct{ to, body string }
	err  error
}

func (f *fakeWA) MarkAsRead(context.Context, string, string) error { return nil }
func (f *fakeWA) SendReply(context.Context, string, string, string, string) error {
	return nil
}

func (f *fakeWA) SendText(_ context.Context, _ /*phoneNumberID*/, to, body string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, struct{ to, body string }{to, body})
	return nil
}

func day(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func ptr(t time.Time) *time.Time { return &t }

func seedUser(t *testing.T, store pkgfinance.Store, prefs domain.NotificationPrefs, entries ...domain.FinancialEntry) {
	t.Helper()
	if err := store.SaveNotificationPrefs(context.Background(), prefs); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := store.SaveEntry(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
}

func newNotifier(store pkgfinance.Store, wa *fakeWA) *Notifier {
	n := New(store, wa, "PHONE_ID", time.UTC)
	n.SetClock(func() time.Time { return day("2026-07-20") })
	return n
}

func TestRunSendsDigestToEnabledUser(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	wa := &fakeWA{}
	seedUser(
		t, store,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
		domain.FinancialEntry{UserID: "u1", EntryID: "e1", Description: "Fornecedor", Amount: 285000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending, DueDate: ptr(day("2026-07-20"))},
	)

	res, err := newNotifier(store, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 1 || len(wa.sent) != 1 {
		t.Fatalf("want 1 send, got res=%+v sent=%d", res, len(wa.sent))
	}
	if wa.sent[0].to != "5511999999999" {
		t.Fatalf("sent to %q", wa.sent[0].to)
	}
}

func TestRunSkipsDisabledOrPhoneless(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	wa := &fakeWA{}
	// disabled
	seedUser(t, store, domain.NotificationPrefs{UserID: "off", WAEnabled: false, Phone: "5511999999999", NotifyDueToday: true},
		domain.FinancialEntry{UserID: "off", EntryID: "e", Amount: 1000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending, DueDate: ptr(day("2026-07-20"))})
	// enabled but no phone
	seedUser(t, store, domain.NotificationPrefs{UserID: "nophone", WAEnabled: true, Phone: "", NotifyDueToday: true},
		domain.FinancialEntry{UserID: "nophone", EntryID: "e", Amount: 1000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending, DueDate: ptr(day("2026-07-20"))})

	res, err := newNotifier(store, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Evaluated != 0 || len(wa.sent) != 0 {
		t.Fatalf("nothing should be sent, got res=%+v sent=%d", res, len(wa.sent))
	}
}

func TestRunDedupesWithinDay(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	wa := &fakeWA{}
	seedUser(
		t, store,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		domain.FinancialEntry{UserID: "u1", EntryID: "e1", Amount: 1000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending, DueDate: ptr(day("2026-07-20"))},
	)
	n := newNotifier(store, wa)

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := n.Run(context.Background()) // second run, same day
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 1 {
		t.Fatalf("second run should not resend, total sent=%d", len(wa.sent))
	}
	if res.Sent != 0 || res.Skipped != 1 {
		t.Fatalf("second run res=%+v", res)
	}
}

func TestRunNoAlertsNoSend(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	wa := &fakeWA{}
	// Enabled user, but the only expense is already paid -> no alert.
	seedUser(
		t, store,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
		domain.FinancialEntry{UserID: "u1", EntryID: "e1", Amount: 1000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid, DueDate: ptr(day("2026-07-20"))},
	)

	res, err := newNotifier(store, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 0 || res.Sent != 0 || res.Skipped != 1 {
		t.Fatalf("want no send, res=%+v sent=%d", res, len(wa.sent))
	}
}
