package notifier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
)

type sentMsg struct{ to, body string }

type fakeWA struct {
	sent []sentMsg
	err  error
}

// digests and billLists split what a run delivered. Every recipient gets one
// digest and, on a day with outflows, one or more list messages behind it — so
// a test that means "the digest" has to say which of the two it is counting.
func (f *fakeWA) digests() []sentMsg   { return f.byKind(false) }
func (f *fakeWA) billLists() []sentMsg { return f.byKind(true) }

func (f *fakeWA) byKind(lists bool) []sentMsg {
	var out []sentMsg
	for _, m := range f.sent {
		if strings.HasPrefix(m.body, "💸") == lists {
			out = append(out, m)
		}
	}
	return out
}

func (f *fakeWA) MarkAsRead(context.Context, string, string) error { return nil }
func (f *fakeWA) SendReply(context.Context, string, string, string, string) error {
	return nil
}

func (f *fakeWA) SendText(_ context.Context, _ /*phoneNumberID*/, to, body string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentMsg{to, body})
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
	if res.Sent != 1 || len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got res=%+v sent=%d", res, len(wa.digests()))
	}
	if wa.digests()[0].to != "5511999999999" {
		t.Fatalf("sent to %q", wa.digests()[0].to)
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
	ctx := context.Background()

	// Opted out entirely.
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "off", WAEnabled: false, Phone: "5511900000001", NotifyDueToday: true})
	// Enabled and has alerts, but the WhatsApp window closed days ago.
	seedUser(t, s, outWindow,
		domain.NotificationPrefs{UserID: "stale", WAEnabled: true, Phone: "5511900000002", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000))
	// Enabled, in-window, and already sent today.
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "done", WAEnabled: true, Phone: "5511900000003", NotifyDueToday: true})
	if err := s.fin.RecordNotificationSent(ctx, "done", runDay.Format("2006-01-02"), runDay); err != nil {
		t.Fatal(err)
	}

	res, err := newNotifier(s, wa).Run(ctx)
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
	if res.SkippedAlreadySent != 1 {
		t.Errorf("SkippedAlreadySent = %d, want 1 (the user already notified today)", res.SkippedAlreadySent)
	}
	if res.SkippedNothingVerified != 0 {
		t.Errorf("SkippedNothingVerified = %d, want 0 — the analysis assembled fine", res.SkippedNothingVerified)
	}
	if res.Sent != 0 || len(wa.sent) != 0 {
		t.Errorf("nothing should have been sent, res=%+v sent=%d", res, len(wa.sent))
	}
	if got := res.Skipped(); got != 1 {
		t.Errorf("Skipped() = %d, want 1 — outside-window is not a skip", got)
	}
}

// brokenForecast is a store whose cash-flow projection fails, which is enough
// to fail the whole analysis (analytics.Assemble reads it).
type brokenForecast struct{ *pkgfinance.InMemoryStore }

func (brokenForecast) CashFlowForecast(context.Context, string, string) ([]pkgfinance.CashFlowPoint, error) {
	return nil, errors.New("dynamo down")
}

// TestRunStaysSilentWhenNothingWasActuallyChecked is the one silence the daily
// digest keeps. The user hears about no bill kind, so the digest may not tell
// them their bills are in order, and the analysis is unavailable, so it has
// nothing to say about the month either. A "resumo do dia" with nothing in it
// would be a message asserting, by its own existence, that someone looked.
func TestRunStaysSilentWhenNothingWasActuallyChecked(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999"},
		dueExpense("Fornecedor", 285000))

	n := New(brokenForecast{s.fin}, s.sessions, wa, "PHONE_ID", "http://localhost:5173", time.UTC, orchestrator.StaticClient{})
	n.SetClock(func() time.Time { return runDay })

	res, err := n.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 0 || res.Sent != 0 {
		t.Fatalf("nothing was verified, so nothing may be claimed: res=%+v sent=%v", res, wa.sent)
	}
	if res.SkippedNothingVerified != 1 {
		t.Fatalf("SkippedNothingVerified = %d, want 1", res.SkippedNothingVerified)
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
	if len(wa.digests()) != 1 {
		t.Fatalf("second run should not resend, total sent=%d", len(wa.digests()))
	}
	// Specifically the dedupe counter: a run that skipped for any other reason
	// would mean the resend guard is not what stopped it.
	if res.Sent != 0 || res.SkippedAlreadySent != 1 || res.SkippedNothingVerified != 0 {
		t.Fatalf("second run res=%+v, want SkippedAlreadySent=1", res)
	}
}

// TestRunSendsCalmDigestWhenNothingIsDue is the behaviour change of issue #35:
// a day with no alert is still a day with an answer. The digest used to skip
// it, so "tudo tranquilo" arrived as no message at all.
func TestRunSendsCalmDigestWhenNothingIsDue(t *testing.T) {
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
	if res.Sent != 1 || len(wa.digests()) != 1 {
		t.Fatalf("a quiet day is still a day to report, res=%+v sent=%d", res, len(wa.digests()))
	}
	body := wa.digests()[0].body
	for _, want := range []string{"Nada vence hoje e não há contas vencidas"} {
		if !strings.Contains(body, want) {
			t.Errorf("calm digest does not say %q:\n%s", want, body)
		}
	}
	if res.SkippedNothingVerified != 0 {
		t.Errorf("SkippedNothingVerified = %d, want 0", res.SkippedNothingVerified)
	}
}

// TestCalmLineClaimsOnlyWhatTheUserSubscribedTo: the absence of an alert is not
// evidence that nothing is due — a user who switched the kind off gets no alert
// either way. Telling them "nada vence hoje" over a bill due today is the one
// sentence in the message that would be false, so it is built from the ledger
// and gated on the preference.
func TestCalmLineClaimsOnlyWhatTheUserSubscribedTo(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	// A bill really is due today; this user does not hear about due-today bills,
	// but does hear about overdue ones (and there are none).
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyOverdue: true},
		dueExpense("Fornecedor", 285000),
	)

	if _, err := newNotifier(s, wa).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.digests()))
	}
	body := wa.digests()[0].body
	if strings.Contains(body, "Nada vence hoje") {
		t.Errorf("digest claims nothing is due while a bill is due today:\n%s", body)
	}
	if !strings.Contains(body, "Não há contas vencidas") {
		t.Errorf("digest drops the claim this user did subscribe to:\n%s", body)
	}
}

// TestGoalAlertOnlyCountsCurrentMonthFaturamento guards two ways the
// goal-reached alert used to fire on money that was never a July sale:
// a May sale big enough to clear July's target on its own (wrong month), and
// a July loan filed under "Outros (Receita)" (real cash, not faturamento).
// Neither must trigger "meta atingida" when July's actual sales are zero.
func TestGoalAlertOnlyCountsCurrentMonthFaturamento(t *testing.T) {
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
	julyLoan := domain.FinancialEntry{
		UserID: shared.FinanceLedgerID, EntryID: "emprestimo-julho", Description: "empréstimo banco",
		Amount: 6000000, TransactionDate: domain.NewCalendarDate(day("2026-07-05")),
		Type: domain.EntryTypeIncome, Category: "outros_receitas",
		PaymentStatus: domain.PaymentStatusPaid, PaymentDate: ptrCD(day("2026-07-05")),
		Source: domain.SourceManual,
	}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyGoal: true},
		maySale, julyLoan,
	)
	if err := s.fin.SaveGoal(ctx, domain.Goal{
		UserID: shared.FinanceLedgerID, Month: "2026-07", RevenueTarget: 5000000,
	}); err != nil {
		t.Fatal(err)
	}

	// The digest goes out every day, so the assertion is about what it says: the
	// "meta atingida" line must not be in it.
	if _, err := newNotifier(s, wa).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.digests()))
	}
	if strings.Contains(wa.digests()[0].body, "Meta de faturamento atingida") {
		t.Fatalf("neither May's sale nor July's loan may count toward July's faturamento goal:\n%s", wa.digests()[0].body)
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
	if res.Sent != 1 || len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got res=%+v sent=%d", res, len(wa.digests()))
	}
	if !strings.HasPrefix(wa.digests()[0].body, gen.reply) {
		t.Fatalf("digest was not humanized: got %q, want it to start with %q", wa.digests()[0].body, gen.reply)
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

// pendingExpense is a bill due today with a description and a supplier of its
// own, so the list has something to render beyond an amount.
func pendingExpense(id, desc, supplier string, amount int64) domain.FinancialEntry {
	e := dueExpense(id, amount)
	e.Description = desc
	e.Supplier = supplier
	return e
}

// TestBillListShipsAsItsOwnMessage: the digest gives the total, the list gives
// the items. They are two messages because the digest is rewritten by the model
// — the one place a line goes missing — and because a long list inside it pushes
// everything else off the screen.
func TestBillListShipsAsItsOwnMessage(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	gen := &fakeGen{reply: "Bom dia! Hoje há pagamentos a fazer. 🙂"}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		pendingExpense("e1", "Distribuidora Santa Cruz", "Santa Cruz LTDA", 285000),
		pendingExpense("e2", "Energia", "", 13500),
		pendingExpense("e3", "Água", "", 1500),
	)

	res, err := newNotifierWithGen(s, wa, gen).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 digest, got %d", len(wa.digests()))
	}
	lists := wa.billLists()
	if len(lists) != 1 {
		t.Fatalf("want the outflows in 1 message of their own, got %d", len(lists))
	}
	if res.BillListsSent != 1 {
		t.Errorf("BillListsSent = %d, want 1", res.BillListsSent)
	}

	list := lists[0].body
	// Every bill, in full, biggest first.
	for _, want := range []string{
		"R$ 2.850,00 — Distribuidora Santa Cruz (Santa Cruz LTDA)",
		"R$ 135,00 — Energia",
		"R$ 15,00 — Água",
	} {
		if !strings.Contains(list, want) {
			t.Errorf("bill list is missing %q:\n%s", want, list)
		}
	}
	if i, j := strings.Index(list, "2.850,00"), strings.Index(list, "135,00"); i > j {
		t.Errorf("want the biggest outflow first:\n%s", list)
	}
	// The header carries the count and the total, so a list split across
	// messages still says how much the day owes.
	if !strings.Contains(list, "(3)") || !strings.Contains(list, "total R$ 3.000,00") {
		t.Errorf("bill list header lost its count or total:\n%s", list)
	}
	// It is the ledger's own copy, not the model's.
	if strings.Contains(list, gen.reply) {
		t.Errorf("the bill list was handed to the model:\n%s", list)
	}
	// And the digest knows the list is coming, so it does not write out a
	// half-remembered version of it above.
	if !strings.Contains(gen.gotSystem, "mensagem seguinte") {
		t.Errorf("the digest prompt does not mention the list that follows:\n%s", gen.gotSystem)
	}
	// A supplier equal to the description would just repeat it.
	if strings.Contains(list, "Energia (Energia)") {
		t.Errorf("supplier repeated the description:\n%s", list)
	}
}

// TestBillListSplitsRatherThanTruncates: a day heavy enough to blow WhatsApp's
// limit continues into further messages. Meta rejects an over-length body
// outright, so the alternative to splitting is not a shorter list — it is no
// list at all.
func TestBillListSplitsRatherThanTruncates(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	prefs := domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true}

	const bills = 120
	entries := make([]domain.FinancialEntry, 0, bills)
	for i := range bills {
		entries = append(entries, pendingExpense(
			fmt.Sprintf("e%03d", i),
			fmt.Sprintf("Fornecedor de material hospitalar número %03d", i),
			"", int64(100*(i+1))))
	}
	seedUser(t, s, inWindow, prefs, entries...)

	res, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	lists := wa.billLists()
	if len(lists) < 2 {
		t.Fatalf("preconditions: %d bills should not fit one message, got %d", bills, len(lists))
	}
	if res.BillListsSent != len(lists) {
		t.Errorf("BillListsSent = %d, want %d", res.BillListsSent, len(lists))
	}

	all := ""
	for i, m := range lists {
		if len(m.body) > maxWhatsAppText {
			t.Errorf("message %d is %d chars, over the %d budget", i+1, len(m.body), maxWhatsAppText)
		}
		all += m.body
	}
	// Nothing was dropped on the way, and the last bill is as present as the
	// first — the whole point of splitting instead of capping.
	for i := range bills {
		want := fmt.Sprintf("hospitalar número %03d", i)
		if got := strings.Count(all, want); got != 1 {
			t.Fatalf("bill %d appears %d times across %d messages", i, got, len(lists))
		}
	}
	// Continuations say which part they are, so a reader knows another is coming.
	if !strings.Contains(lists[1].body, fmt.Sprintf("continuação (2/%d)", len(lists))) {
		t.Errorf("continuation is not numbered:\n%s", lists[1].body)
	}
}

// TestBillListRespectsTheDueTodayOptOut: the list is the detail behind the
// due-today alert. Someone who switched that alert off must not receive it by
// another route.
func TestBillListRespectsTheDueTodayOptOut(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyOverdue: true},
		pendingExpense("e1", "Distribuidora", "", 285000),
	)

	res, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.billLists()) != 0 || res.BillListsSent != 0 {
		t.Fatalf("bill list sent to someone who opted out: %v", wa.billLists())
	}
	if len(wa.digests()) != 1 {
		t.Fatalf("the digest itself must still go out, got %d", len(wa.digests()))
	}
}

// TestDigestPromptOnlyAnnouncesAListThatIsComing: the sentence pointing at the
// next message is written only when there is one, or the digest sends the
// reader looking for something that never arrives.
func TestDigestPromptOnlyAnnouncesAListThatIsComing(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	gen := &fakeGen{reply: "Bom dia! Está tudo em ordem por aqui. 🙂"}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
	)

	if _, err := newNotifierWithGen(s, wa, gen).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gen.gotSystem, "mensagem seguinte") {
		t.Errorf("nothing is due, so no list follows — the prompt must not promise one:\n%s", gen.gotSystem)
	}
}

// TestNoBillListOnAQuietDay: nothing due, nothing to list. The digest already
// says the day is clear, and an empty "saídas previstas" message would only
// make the reader look for what is not there.
func TestNoBillListOnAQuietDay(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true, NotifyOverdue: true},
	)

	res, err := newNotifier(s, wa).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wa.sent) != 1 || res.BillListsSent != 0 {
		t.Fatalf("want the digest alone, got %d messages (%+v)", len(wa.sent), res)
	}
}

// TestDigestSurvivesAFailedBillList: the two messages fail independently. A
// list that could not be delivered must not un-send the digest, nor make
// tomorrow's run repeat it.
func TestDigestSurvivesAFailedBillList(t *testing.T) {
	s := newStores()
	wa := &failAfterFirst{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		pendingExpense("e1", "Distribuidora", "", 285000),
	)

	n := New(s.fin, s.sessions, wa, "PHONE_ID", "http://localhost:5173", time.UTC, orchestrator.StaticClient{})
	n.SetClock(func() time.Time { return runDay })

	res, err := n.Run(context.Background())
	if err == nil {
		t.Fatal("a failed list send must be reported as an error")
	}
	if res.Sent != 1 || res.Errors != 1 {
		t.Fatalf("res=%+v, want the digest counted as sent and one error", res)
	}
	sent, lerr := s.fin.NotificationSent(context.Background(), "u1", runDay.Format("2006-01-02"))
	if lerr != nil {
		t.Fatal(lerr)
	}
	if !sent {
		t.Error("the digest went out but was not recorded — tomorrow's run would send it again")
	}
}

// failAfterFirst delivers the digest and then fails, which is the only way the
// two messages can come apart in production.
type failAfterFirst struct{ fakeWA }

func (f *failAfterFirst) SendText(ctx context.Context, phoneNumberID, to, body string) error {
	if len(f.sent) > 0 {
		return errors.New("meta rejected the message")
	}
	return f.fakeWA.SendText(ctx, phoneNumberID, to, body)
}

// TestDigestFeedsTheModelTheWholeInsightsJSON is issue #35's other half: the
// model used to receive a handful of rendered lines, so nothing the analysis
// knew but did not print could ever reach the reader. It now writes from the
// same insights JSON the analysis page renders.
//
// The split matters as much as the payload. The draft stays in the user message
// because the no-LLM path echoes that field back verbatim — see
// TestDigestWithoutAModelShipsTheDraftAndNotTheJSON.
func TestDigestFeedsTheModelTheWholeInsightsJSON(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	gen := &fakeGen{reply: "Bom dia! Hoje vence uma conta de R$ 2.850,00. 🙂"}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000),
	)

	if _, err := newNotifierWithGen(s, wa, gen).Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Figures the rendered lines never carried, and which the message can now
	// draw on: the cash runway, the month's commitments and the week's pace.
	for _, key := range []string{"\"caixa\"", "\"projecao_do_mes\"", "\"compromissos_situacao\"", "\"semana\""} {
		if !strings.Contains(gen.gotSystem, key) {
			t.Errorf("insights JSON is missing %s:\n%s", key, gen.gotSystem)
		}
	}
	// Quoting is allowed, arithmetic is not — the same prohibition ADR-019 puts
	// on the per-day target, and the reason the whole payload can be handed over
	// at all.
	if !strings.Contains(gen.gotSystem, "Nunca some, subtraia, divida ou calcule") {
		t.Errorf("prompt does not forbid deriving new numbers:\n%s", gen.gotSystem)
	}
	// Affordances for a caller with tools have no meaning in a one-shot rewrite,
	// and "chame get_meta_do_dia" is an instruction the writer cannot follow.
	for _, toolOnly := range []string{"secoes_disponiveis", "get_meta_do_dia"} {
		if strings.Contains(gen.gotSystem, toolOnly) {
			t.Errorf("digest payload carries the tool affordance %q:\n%s", toolOnly, gen.gotSystem)
		}
	}
	// The draft — the message that ships if the model fails — is what the user
	// message carries, and it is prose rather than JSON.
	if !strings.Contains(gen.gotMessage, "Farmácia Financeira") || strings.Contains(gen.gotMessage, "{\"") {
		t.Errorf("the user message must be the ready-to-send draft, got:\n%s", gen.gotMessage)
	}
}

// echoGen mirrors the orchestrator's no-LLM digest fallback: it returns the
// caller's own message unchanged. Whatever the notifier puts in that field is
// what a pharmacy with no GEMINI_API_KEY receives on WhatsApp.
type echoGen struct{}

func (echoGen) Generate(_ context.Context, input orchestrator.Input) (orchestrator.Output, error) {
	return orchestrator.Output{Text: input.UserMessage.Text}, nil
}

func TestDigestWithoutAModelShipsTheDraftAndNotTheJSON(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	seedUser(
		t, s, inWindow,
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyDueToday: true},
		dueExpense("Fornecedor", 285000),
	)

	if _, err := newNotifierWithGen(s, wa, echoGen{}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.digests()))
	}
	body := wa.digests()[0].body
	if strings.Contains(body, "{\"") || strings.Contains(body, "projecao_do_mes") {
		t.Fatalf("raw insights JSON shipped to WhatsApp:\n%s", body)
	}
	if !strings.Contains(body, "vence hoje") {
		t.Fatalf("the draft did not survive the echo path:\n%s", body)
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
	if res.Sent != 1 || len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got res=%+v sent=%d", res, len(wa.digests()))
	}
	if !strings.Contains(wa.digests()[0].body, "Farmácia Financeira") {
		t.Fatalf("expected the static digest fallback, got %q", wa.digests()[0].body)
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
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.digests()))
	}
	body := wa.digests()[0].body
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
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.digests()))
	}
	if strings.Contains(wa.digests()[0].body, "Acesse a análise completa") {
		t.Fatalf("call-to-action shipped without a URL: %q", wa.digests()[0].body)
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
	if n := strings.Count(wa.digests()[0].body, "/analise"); n != 1 {
		t.Fatalf("want the dashboard link exactly once, got %d in %q", n, wa.digests()[0].body)
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
	if res.Sent != 2 || len(wa.digests()) != 2 {
		t.Fatalf("want 2 independent sends, got res=%+v sent=%d", res, len(wa.digests()))
	}
	digests := wa.digests()
	gotPhones := map[string]bool{digests[0].to: true, digests[1].to: true}
	if !gotPhones["5511900000001"] || !gotPhones["5511900000002"] {
		t.Fatalf("want both recipients to receive their own digest, got %v", digests)
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
	if res.Sent != 1 || len(wa.digests()) != 1 || wa.digests()[0].to != "5511900000002" {
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
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.digests()))
	}

	body := wa.digests()[0].body
	// What has to be dealt with from here leads the message, and the bill with
	// a deadline leads that.
	agora := strings.Index(body, "A partir de agora")
	ontem := strings.Index(body, "Como fechamos até ontem")
	if agora < 0 || ontem < 0 {
		t.Fatalf("digest lost one of its two halves:\n%s", body)
	}
	if agora > ontem {
		t.Errorf("what is still to do must come before how the month has gone:\n%s", body)
	}
	if !strings.Contains(body, "vence hoje") {
		t.Errorf("digest lost its alert:\n%s", body)
	}
	// The retrospective half names the window it measured, so a percentage in
	// it can never read as a whole month.
	if !strings.Contains(body, "Saúde do mês até ontem (dia ") {
		t.Errorf("digest does not say which days its verdict covers:\n%s", body)
	}
	// R$1.000 of a R$50.000 target with most of the month gone.
	if !strings.Contains(body, "R$ 50.000,00") {
		t.Errorf("digest does not say what the goal still needs:\n%s", body)
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
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.digests()))
	}
	if !strings.Contains(wa.digests()[0].body, "vence hoje") {
		t.Errorf("digest lost its alert:\n%s", wa.digests()[0].body)
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

// TestDigestOnTheFirstOfTheMonthTalksOnlyAboutWhatIsAhead reproduces the
// message that went out on 1 August: two overdue bills, and then "saúde
// crítica", "fluxo de caixa negativo" and "receita caiu 100% vs mês passado" —
// about a month whose only content that morning was its own booked expenses.
//
// The overdue bills were the one true thing in it, and they belong to what has
// to be dealt with from here.
func TestDigestOnTheFirstOfTheMonthTalksOnlyAboutWhatIsAhead(t *testing.T) {
	s := newStores()
	wa := &fakeWA{}
	ctx := context.Background()

	firstOfMonth := day("2026-08-01")

	overdue := func(id string, dueOn string, amount int64) domain.FinancialEntry {
		d := day(dueOn)
		return domain.FinancialEntry{
			UserID: shared.FinanceLedgerID, EntryID: domain.EntryID(id), Description: id, Amount: amount,
			TransactionDate: domain.NewCalendarDate(d), Type: domain.EntryTypeExpense,
			PaymentStatus: domain.PaymentStatusPending, DueDate: ptrCD(d), Source: domain.SourceManual,
		}
	}
	// July traded every single day; August has nothing but its own bills.
	entries := []domain.FinancialEntry{
		overdue("Assinatura do app painel financeiro", "2026-07-31", 9900),
		overdue("Folha de pagamento", "2026-07-30", 900000),
		overdue("Aluguel de agosto", "2026-08-05", 300000),
	}
	for d := 1; d <= 31; d++ {
		date := day("2026-07-01").AddDate(0, 0, d-1)
		entries = append(entries, domain.FinancialEntry{
			UserID: shared.FinanceLedgerID, EntryID: domain.EntryID("venda-" + date.Format("02")),
			Description: "venda", Amount: 50000, TransactionDate: domain.NewCalendarDate(date),
			Type: domain.EntryTypeIncome, Category: "venda_balcao", Origin: domain.OriginVenda,
			PaymentStatus: domain.PaymentStatusPaid, PaymentDate: ptrCD(date), Source: domain.SourceManual,
		})
	}

	seedUser(
		t, s, firstOfMonth.Add(-6*time.Hour),
		domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999999999", NotifyOverdue: true},
		entries...,
	)
	if err := s.fin.SaveGoal(ctx, domain.Goal{
		UserID: shared.FinanceLedgerID, Month: "2026-08", RevenueTarget: 1550000,
	}); err != nil {
		t.Fatal(err)
	}

	n := New(s.fin, s.sessions, wa, "PHONE_ID", "http://localhost:5173", time.UTC, orchestrator.StaticClient{})
	n.SetClock(func() time.Time { return firstOfMonth.Add(9 * time.Hour) })
	if _, err := n.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(wa.digests()) != 1 {
		t.Fatalf("want 1 send, got %d", len(wa.digests()))
	}
	body := wa.digests()[0].body

	// The pendências are real and still lead the message.
	if !strings.Contains(body, "Folha de pagamento está vencida") {
		t.Errorf("digest lost the overdue bills:\n%s", body)
	}
	// The month has one day, it has not been traded, and no percentage may be
	// quoted about it.
	for _, banned := range []string{"%", "Crítico", "Fluxo negativo", "caiu"} {
		if strings.Contains(body, banned) {
			t.Errorf("digest still judges an untraded month (%q):\n%s", banned, body)
		}
	}
	if !strings.Contains(body, "começando") {
		t.Errorf("digest does not say the month is just starting:\n%s", body)
	}
	// The recommendation is the only thing ahead of the month start.
	if !strings.Contains(body, "Ritmo suficiente") {
		t.Errorf("digest does not price the target over the whole month ahead:\n%s", body)
	}
}
