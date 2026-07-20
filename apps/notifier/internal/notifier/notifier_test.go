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

// runDay is the fixed "now" the test notifier uses. inWindow / outWindow are
// last-inbound timestamps that respectively fall inside / outside WhatsApp's
// 24h customer-service window relative to runDay.
var (
	runDay    = day("2026-07-20")
	inWindow  = day("2026-07-19").Add(12 * time.Hour) // 12h ago
	outWindow = day("2026-07-17")                     // 3 days ago
)

// seedUser saves prefs + entries and, when inboundAt is non-zero, records it as
// the phone's last inbound message (controls the 24h window).
func seedUser(t *testing.T, store pkgfinance.Store, inboundAt time.Time, prefs domain.NotificationPrefs, entries ...domain.FinancialEntry) {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveNotificationPrefs(ctx, prefs); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := store.SaveEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if !inboundAt.IsZero() && prefs.Phone != "" {
		if err := store.RecordInboundMessage(ctx, prefs.Phone, inboundAt); err != nil {
			t.Fatal(err)
		}
	}
}

func newNotifier(store pkgfinance.Store, wa *fakeWA) *Notifier {
	n := New(store, wa, "PHONE_ID", time.UTC)
	n.SetClock(func() time.Time { return runDay })
	return n
}

func dueExpense(id string, amount int64) domain.FinancialEntry {
	return domain.FinancialEntry{
		UserID: "u1", EntryID: id, Description: id, Amount: amount,
		Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending,
		DueDate: ptr(runDay),
	}
}

func TestRunSendsDigestToEnabledUserInWindow(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	wa := &fakeWA{}
	seedUser(
		t, store, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
		dueExpense("Fornecedor", 285000),
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

func TestRunSkipsOutsideCustomerServiceWindow(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	wa := &fakeWA{}
	// Enabled, with a real due-today alert, but last messaged us 3 days ago.
	seedUser(
		t, store, outWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000),
	)

	res, err := newNotifier(store, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 0 {
		t.Fatalf("must not send outside the 24h window, sent=%d", len(wa.sent))
	}
	if res.Evaluated != 1 || res.OutsideWindow != 1 || res.Sent != 0 {
		t.Fatalf("res=%+v, want Evaluated=1 OutsideWindow=1 Sent=0", res)
	}
}

func TestRunSkipsWhenNeverMessagedUs(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	wa := &fakeWA{}
	// No inbound recorded at all (inboundAt zero) -> never in the window.
	seedUser(
		t, store, time.Time{},
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000),
	)

	res, err := newNotifier(store, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 0 || res.OutsideWindow != 1 {
		t.Fatalf("res=%+v sent=%d, want no send and OutsideWindow=1", res, len(wa.sent))
	}
}

func TestRunSkipsDisabledOrPhoneless(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	wa := &fakeWA{}
	seedUser(t, store, inWindow,
		domain.NotificationPrefs{UserID: "off", WAEnabled: false, Phone: "5511999999999", NotifyDueToday: true},
		domain.FinancialEntry{UserID: "off", EntryID: "e", Amount: 1000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending, DueDate: ptr(runDay)})
	seedUser(t, store, inWindow,
		domain.NotificationPrefs{UserID: "nophone", WAEnabled: true, Phone: "", NotifyDueToday: true},
		domain.FinancialEntry{UserID: "nophone", EntryID: "e", Amount: 1000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending, DueDate: ptr(runDay)})

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
		t, store, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("e1", 1000),
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
	// In-window and enabled, but the only expense is already paid -> no alert.
	seedUser(
		t, store, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
		domain.FinancialEntry{UserID: "u1", EntryID: "e1", Amount: 1000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid, DueDate: ptr(runDay)},
	)

	res, err := newNotifier(store, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 0 || res.Sent != 0 || res.Skipped != 1 {
		t.Fatalf("want no send, res=%+v sent=%d", res, len(wa.sent))
	}
}
