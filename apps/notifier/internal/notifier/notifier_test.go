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
	// Both users must be accounted for. A run that reports two prefs rows and
	// zero of everything else gives no clue that opt-in was the blocker.
	if res.Prefs != 2 || res.NotOptedIn != 2 {
		t.Errorf("res=%+v, want Prefs=2 NotOptedIn=2", res)
	}
}

// The point of separating these counters is that each one has a different fix.
// A run summary that cannot tell them apart is what made a silent day
// undiagnosable in the first place.
func TestRunDistinguishesEveryNonDeliveryReason(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}

	// Opted out entirely.
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "off", WAEnabled: false, Phone: "5511900000001", NotifyDueToday: true})
	// Enabled and has alerts, but the WhatsApp window closed days ago.
	seedUser(t, s, outWindow,
		domain.NotificationPrefs{UserID: "stale", WAEnabled: true, Phone: "5511900000002", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000))
	// Enabled and in-window, but subscribed to no alert kind, so nothing fires.
	// (Withholding entries would not work: every user reads the same shared
	// ledger, so "stale"'s overdue bill is visible to this user too.)
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "quiet", WAEnabled: true, Phone: "5511900000003"})

	res, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if res.Prefs != 3 {
		t.Errorf("Prefs = %d, want 3", res.Prefs)
	}
	if res.NotOptedIn != 1 {
		t.Errorf("NotOptedIn = %d, want 1 (the disabled user)", res.NotOptedIn)
	}
	if res.OutsideWindow != 1 {
		t.Errorf("OutsideWindow = %d, want 1 (the stale session)", res.OutsideWindow)
	}
	if res.SkippedNoAlerts != 1 {
		t.Errorf("SkippedNoAlerts = %d, want 1 (the quiet user)", res.SkippedNoAlerts)
	}
	if res.SkippedAlreadySent != 0 {
		t.Errorf("SkippedAlreadySent = %d, want 0 — nothing was sent today yet", res.SkippedAlreadySent)
	}
	if res.Sent != 0 || len(wa.sent) != 0 {
		t.Errorf("nothing should have been sent, res=%+v sent=%d", res, len(wa.sent))
	}
	if got := res.Skipped(); got != 1 {
		t.Errorf("Skipped() = %d, want 1 — outside-window is not a skip", got)
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
	// Specifically the dedupe counter: a run that skipped for any other reason
	// would mean the resend guard is not what stopped it.
	if res.Sent != 0 || res.SkippedAlreadySent != 1 || res.SkippedNoAlerts != 0 {
		t.Fatalf("second run res=%+v, want SkippedAlreadySent=1", res)
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
	if len(wa.sent) != 0 || res.Sent != 0 || res.SkippedNoAlerts != 1 || res.SkippedAlreadySent != 0 {
		t.Fatalf("want no send for lack of alerts, res=%+v sent=%d", res, len(wa.sent))
	}
}

// TestGoalAlertOnlyCountsCurrentMonthRevenue guards against the goal-reached
// alert summing revenue across the whole overdue lookback window (several
// months) instead of just the month the goal was set for. A May sale big
// enough to clear July's target on its own must not fire "meta atingida" in
// July, when July itself has no revenue at all.
func TestGoalAlertOnlyCountsCurrentMonthRevenue(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	ctx := context.Background()

	maySale := domain.FinancialEntry{
		UserID: shared.FinanceLedgerID, EntryID: "venda-maio", Description: "venda",
		Amount: 6000000, TransactionDate: domain.NewCalendarDate(day("2026-05-15")),
		Type: domain.EntryTypeIncome, Category: "venda_balcao",
		PaymentStatus: domain.PaymentStatusPaid, PaymentDate: ptrCD(day("2026-05-15")),
		Source: domain.SourceManual,
	}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyGoal: true},
		maySale,
	)
	if err := s.fin.SaveGoal(ctx, domain.Goal{
		UserID: shared.FinanceLedgerID, Month: "2026-07", RevenueTarget: 5000000,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := newNotifier(s, wa).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 0 || len(wa.sent) != 0 {
		t.Fatalf("May's revenue must not count toward July's goal, res=%+v sent=%d", res, len(wa.sent))
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
	if !strings.HasPrefix(wa.sent[0].body, gen.reply) {
		t.Fatalf("digest was not humanized: got %q, want it to start with %q", wa.sent[0].body, gen.reply)
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

// TestDigestReplacesInventedLinkPlaceholderWithRealURL is the regression test
// for the "[Link para o dashboard]" digest: the model was told to preserve a
// link the draft never carried (the notifier Lambda had no DASHBOARD_URL), so
// it invented a placeholder. The real link is now appended after generation and
// any invented stand-in is stripped.
func TestDigestReplacesInventedLinkPlaceholderWithRealURL(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	gen := &fakeGen{reply: "Olá! Tudo bem?\n\nHoje temos um compromisso:\n• *Pagamento de R$ 100,00* com vencimento para hoje.\n\n" +
		"Para mais detalhes, acesse seu dashboard aqui: [Link para o dashboard]"}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 10000),
	)

	if _, err := newNotifierWithGen(s, wa, gen).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.sent))
	}
	body := wa.sent[0].body
	if strings.Contains(body, "[Link para o dashboard]") {
		t.Fatalf("placeholder shipped to the user: %q", body)
	}
	if !strings.Contains(body, "http://localhost:5173/analise") {
		t.Fatalf("real dashboard link missing: %q", body)
	}
	// The alert itself must survive the placeholder cleanup.
	if !strings.Contains(body, "R$ 100,00") {
		t.Fatalf("alert content lost while stripping the placeholder: %q", body)
	}
}

// TestDigestUnwrapsMarkdownLinks: WhatsApp renders no markdown, so a real URL
// wrapped as "[texto](url)" would ship with its brackets showing.
func TestDigestUnwrapsMarkdownLinks(t *testing.T) {
	got := stripInventedLinks("Confira em [nosso painel](https://exemplo.com/analise) hoje.")
	want := "Confira em https://exemplo.com/analise hoje."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestDigestOmitsCallToActionWhenNoDashboardURL: with DASHBOARD_URL unset the
// digest must simply have no link, never a stand-in for one.
func TestDigestOmitsCallToActionWhenNoDashboardURL(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 10000),
	)
	n := New(s.fin, s.sessions, wa, "PHONE_ID", "", time.UTC, orchestrator.StaticClient{})
	n.SetClock(func() time.Time { return runDay })

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.sent))
	}
	if strings.Contains(wa.sent[0].body, "Acesse a análise completa") {
		t.Fatalf("call-to-action shipped without a URL: %q", wa.sent[0].body)
	}
}

// TestStaticDigestCarriesLinkExactlyOnce guards the fallback path against a
// duplicated call-to-action now that the link is appended after the body.
func TestStaticDigestCarriesLinkExactlyOnce(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	gen := &fakeGen{err: errors.New("gemini down")}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 10000),
	)

	if _, err := newNotifierWithGen(s, wa, gen).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(wa.sent[0].body, "/analise"); n != 1 {
		t.Fatalf("want the dashboard link exactly once, got %d in %q", n, wa.sent[0].body)
	}
}

// TestDashboardLinkTrimsTrailingSlash keeps a configured "https://host/" from
// producing "https://host//analise".
func TestDashboardLinkTrimsTrailingSlash(t *testing.T) {
	if got := dashboardLink("https://dash.example.com/"); !strings.HasSuffix(got, "https://dash.example.com/analise") {
		t.Fatalf("got %q", got)
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

func TestDigestCarriesTheMonthsAnalysis(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	ctx := context.Background()

	// A month running well behind its goal, so the analysis has something to
	// say beyond the bill that is due today.
	sale := domain.FinancialEntry{
		UserID: shared.FinanceLedgerID, EntryID: "venda", Description: "venda",
		Amount: 100000, TransactionDate: domain.NewCalendarDate(day("2026-07-02")),
		Type: domain.EntryTypeIncome, Category: "venda_balcao",
		PaymentStatus: domain.PaymentStatusPaid, PaymentDate: ptrCD(day("2026-07-02")),
		Source: domain.SourceManual,
	}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000), sale,
	)
	if err := s.fin.SaveGoal(ctx, domain.Goal{
		UserID: shared.FinanceLedgerID, Month: "2026-07", RevenueTarget: 5000000,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := newNotifier(s, wa).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.sent))
	}

	body := wa.sent[0].body
	// The alerts still lead — the analysis is context, not a replacement.
	if !strings.Contains(body, "vence hoje") {
		t.Errorf("digest lost its alert:\n%s", body)
	}
	if !strings.Contains(body, "Como está o mês") {
		t.Errorf("digest has no analysis section:\n%s", body)
	}
	if !strings.Contains(body, "Saúde do mês:") {
		t.Errorf("digest has no health status:\n%s", body)
	}
	// R$1.000 of a R$50.000 target with most of the month gone.
	if !strings.Contains(body, "/dia") {
		t.Errorf("digest does not say what the goal still needs per day:\n%s", body)
	}
}

func TestDigestStillSendsWhenTheAnalysisIsEmpty(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000),
	)

	if _, err := newNotifier(s, wa).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.sent))
	}
	if !strings.Contains(wa.sent[0].body, "vence hoje") {
		t.Errorf("digest lost its alert:\n%s", wa.sent[0].body)
	}
}

func TestRunPersistsInsightSnapshot(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000),
	)

	_, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Snapshot should be persisted for today's date.
	date := runDay.Format("2006-01-02")
	snap, err := s.fin.GetInsightSnapshot(context.Background(), shared.FinanceLedgerID, date)
	if err != nil {
		t.Fatalf("snapshot not persisted: %v", err)
	}
	if len(snap.Snapshot) == 0 {
		t.Error("snapshot is empty")
	}
	if snap.ComputedAt.IsZero() {
		t.Error("computedAt is zero")
	}
}
