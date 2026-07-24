package notifier

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
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

func ptrCD(t time.Time) *domain.CalendarDate { cd := domain.NewCalendarDate(t); return &cd }

// runDay is the fixed "now" the test notifier uses. inWindow / outWindow are
// last-inbound timestamps whose sessions are respectively still open / expired
// (wasession.Window) as of runDay.
var (
	runDay    = day("2026-07-20")
	inWindow  = day("2026-07-19").Add(12 * time.Hour) // session open at runDay
	outWindow = day("2026-07-17")                     // session long expired
)

type stores struct {
	fin      *pkgfinance.InMemoryStore
	sessions *wasession.InMemoryStore
}

func newStores() stores {
	return stores{fin: pkgfinance.NewInMemoryStore(), sessions: wasession.NewInMemoryStore()}
}

// seedUser saves prefs + entries and, when inboundAt is non-zero, records it as
// the phone's last inbound message (which controls the 24h window).
func seedUser(t *testing.T, s stores, inboundAt time.Time, prefs domain.NotificationPrefs, entries ...domain.FinancialEntry) {
	t.Helper()
	ctx := context.Background()
	if err := s.fin.SaveNotificationPrefs(ctx, prefs); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := s.fin.SaveEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if !inboundAt.IsZero() && prefs.Phone != "" {
		if err := s.sessions.RecordInbound(ctx, prefs.Phone, inboundAt); err != nil {
			t.Fatal(err)
		}
	}
}

func newNotifier(s stores, wa *fakeWA) *Notifier {
	return newNotifierWithGen(s, wa, orchestrator.StaticClient{})
}

func newNotifierWithGen(s stores, wa *fakeWA, gen orchestrator.TextGenerator) *Notifier {
	n := New(s.fin, s.sessions, wa, "PHONE_ID", "http://localhost:5173", time.UTC, gen)
	n.SetClock(func() time.Time { return runDay })
	return n
}

// fakeGen is a TextGenerator that records what buildDigest hands it and returns
// a canned reply (or error), so tests can assert the digest is humanized rather
// than sent as the raw static template.
type fakeGen struct {
	reply      string
	err        error
	gotSystem  string
	gotMessage string
}

func (f *fakeGen) Generate(_ context.Context, input orchestrator.Input) (orchestrator.Output, error) {
	f.gotSystem = input.SystemPrompt
	f.gotMessage = input.UserMessage.Text
	if f.err != nil {
		return orchestrator.Output{}, f.err
	}
	return orchestrator.Output{Text: f.reply}, nil
}

// dueExpense creates an entry on the one shared financial ledger — every
// recipient's prefs point at their own real Cognito user, but they all read
// this same ledger.
func dueExpense(id string, amount int64) domain.FinancialEntry {
	cd := domain.NewCalendarDate(runDay)
	return domain.FinancialEntry{
		UserID: shared.FinanceLedgerID, EntryID: domain.EntryID(id), Description: id, Amount: amount,
		TransactionDate: cd, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending,
		DueDate: ptrCD(runDay), Source: domain.SourceManual,
	}
}

func TestRunSendsDigestToEnabledUserInWindow(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
		dueExpense("Fornecedor", 285000),
	)

	res, err := newNotifier(s, wa).Run(context.Background())
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
	s := newStores()
	wa := &fakeWA{}
	// Enabled, with a real due-today alert, but last messaged us days ago.
	seedUser(
		t, s, outWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000),
	)

	res, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 0 {
		t.Fatalf("must not send outside the window, sent=%d", len(wa.sent))
	}
	if res.Evaluated != 1 || res.OutsideWindow != 1 || res.Sent != 0 {
		t.Fatalf("res=%+v, want Evaluated=1 OutsideWindow=1 Sent=0", res)
	}
}

func TestRunSkipsWhenNeverMessagedUs(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	// No inbound recorded at all -> no session -> outside the window.
	seedUser(
		t, s, time.Time{},
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000),
	)

	res, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 0 || res.OutsideWindow != 1 {
		t.Fatalf("res=%+v sent=%d, want no send and OutsideWindow=1", res, len(wa.sent))
	}
}

func TestRunSkipsDisabledOrPhoneless(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "off", WAEnabled: false, Phone: "5511999999999", NotifyDueToday: true})
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "nophone", WAEnabled: true, Phone: "", NotifyDueToday: true})

	res, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Evaluated != 0 || len(wa.sent) != 0 {
		t.Fatalf("nothing should be sent, got res=%+v sent=%d", res, len(wa.sent))
	}
}

func TestRunDedupesWithinDay(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("e1", 1000),
	)
	n := newNotifier(s, wa)

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
	s := newStores()
	wa := &fakeWA{}
	// In-window and enabled, but the only expense is already paid -> no alert.
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
		domain.FinancialEntry{UserID: shared.FinanceLedgerID, EntryID: domain.EntryID("e1"), TransactionDate: domain.NewCalendarDate(runDay), Amount: 1000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid, PaymentDate: ptrCD(runDay), DueDate: ptrCD(runDay), Source: domain.SourceManual},
	)

	res, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 0 || res.Sent != 0 || res.Skipped != 1 {
		t.Fatalf("want no send, res=%+v sent=%d", res, len(wa.sent))
	}
}

// TestRunSendsHumanizedDigestWhenGeneratorSucceeds is the regression test for
// issue #36: the digest must actually send the model's rewritten text, feeding
// the generator a non-empty draft plus the system prompt. Before the fix the
// generator errored on the (always empty) history and the humanized text was
// never sent.
func TestRunSendsHumanizedDigestWhenGeneratorSucceeds(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	gen := &fakeGen{reply: "Olá! Você tem uma conta de R$2.850,00 vencendo hoje. 🙂"}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
		dueExpense("Fornecedor", 285000),
	)

	res, err := newNotifierWithGen(s, wa, gen).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 1 || len(wa.sent) != 1 {
		t.Fatalf("want 1 send, got res=%+v sent=%d", res, len(wa.sent))
	}
	if wa.sent[0].body != gen.reply {
		t.Fatalf("digest was not humanized: got %q, want %q", wa.sent[0].body, gen.reply)
	}
	// The generator must receive both the system prompt and a non-empty draft to
	// rewrite — the fields the old agent-based path dropped.
	if gen.gotSystem == "" {
		t.Fatal("generator received no system prompt")
	}
	if gen.gotMessage == "" {
		t.Fatal("generator received an empty draft to rewrite")
	}
}

// TestRunFallsBackToStaticDigestOnGeneratorError proves the static template is
// still the safety net: a failing generator must not block the alert.
func TestRunFallsBackToStaticDigestOnGeneratorError(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	gen := &fakeGen{err: errors.New("gemini down")}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
		dueExpense("Fornecedor", 285000),
	)

	res, err := newNotifierWithGen(s, wa, gen).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 1 || len(wa.sent) != 1 {
		t.Fatalf("want 1 send, got res=%+v sent=%d", res, len(wa.sent))
	}
	if !strings.Contains(wa.sent[0].body, "Farmácia Financeira") {
		t.Fatalf("expected the static digest fallback, got %q", wa.sent[0].body)
	}
}

// TestRunNotifiesMultipleCognitoUsersFromSharedLedger is the regression test
// for the identity-collapsing bug: two distinct Cognito users, each with
// their own prefs/phone, both watching the one shared financial ledger, must
// each get their own digest.
func TestRunNotifiesMultipleCognitoUsersFromSharedLedger(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	ctx := context.Background()

	if err := s.fin.SaveEntry(ctx, dueExpense("Fornecedor", 285000)); err != nil {
		t.Fatal(err)
	}
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511900000001", NotifyDueToday: true})
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "u2", WAEnabled: true, Phone: "5511900000002", NotifyDueToday: true})

	res, err := newNotifier(s, wa).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 2 || len(wa.sent) != 2 {
		t.Fatalf("want 2 independent sends, got res=%+v sent=%d", res, len(wa.sent))
	}
	gotPhones := map[string]bool{wa.sent[0].to: true, wa.sent[1].to: true}
	if !gotPhones["5511900000001"] || !gotPhones["5511900000002"] {
		t.Fatalf("want both recipients to receive their own digest, got %v", wa.sent)
	}
}

// TestRunDedupeIsPerRecipientNotPerLedger seeds two recipients on the same
// shared ledger, but only one of them as already notified today — a single
// Run must still send to the other. Dedupe keys on the real recipient's
// UserID, not on the shared ledger they both read from.
func TestRunDedupeIsPerRecipientNotPerLedger(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	ctx := context.Background()

	if err := s.fin.SaveEntry(ctx, dueExpense("Fornecedor", 285000)); err != nil {
		t.Fatal(err)
	}
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511900000001", NotifyDueToday: true})
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "u2", WAEnabled: true, Phone: "5511900000002", NotifyDueToday: true})
	if err := s.fin.RecordNotificationSent(ctx, "u1", runDay.Format("2006-01-02"), runDay); err != nil {
		t.Fatal(err)
	}

	res, err := newNotifier(s, wa).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 1 || len(wa.sent) != 1 || wa.sent[0].to != "5511900000002" {
		t.Fatalf("want only u2 sent (u1 already deduped), got res=%+v sent=%v", res, wa.sent)
	}
}
