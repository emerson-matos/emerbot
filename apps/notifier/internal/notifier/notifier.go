// Package notifier evaluates each user's alerts once and sends a single daily
// WhatsApp digest, deduplicated per day. It is the scheduled (EventBridge) twin
// of the dashboard's notification bell: same rules (via packages/notifications),
// different delivery channel.
//
// The digest goes out every day, not only when a rule fires. It is a report on
// the morning, written from the month's analysis (packages/finance/analytics) —
// the same insights the /analise page renders — and a morning with nothing to
// flag is one of its answers, not a reason to withhold it. See ADR-023.
//
// There is nothing to opt into and nothing to switch off. The pharmacy wants
// these messages, so every registered recipient with a phone gets all of them —
// see domain.NotificationPrefs, which is now an address book and not a set of
// preferences.
package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/finance/analytics"
	"github.com/emerson/emerbot/packages/notifications"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/whatsapp"
)

// OverdueLookbackMonths bounds how far back the entries query reaches when
// looking for still-pending bills — matches the web hook's window.
const OverdueLookbackMonths = 3

// LedgerReader is the slice of the finance store the notifier needs: it reads
// entries and goals, and keeps its own per-day delivery log. It writes no
// financial data at all, and declaring that here makes it impossible to start.
type LedgerReader interface {
	ListEntries(ctx context.Context, userID string, filter pkgfinance.EntryFilter) ([]domain.FinancialEntry, error)
	GetGoal(ctx context.Context, userID, month string) (domain.Goal, error)
	ListNotificationPrefs(ctx context.Context) ([]domain.NotificationPrefs, error)
	NotificationSent(ctx context.Context, userID, key string) (bool, error)
	RecordNotificationSent(ctx context.Context, userID, key string, sentAt time.Time) error
	// The digest embeds the month's analysis, which reads a trailing window of
	// summaries, the ledger's cash-flow projection and the user's categories —
	// the digest names the kinds of sale behind the month's faturamento, and a
	// category the user created is only named in their own catalog.
	MultiMonthlySummary(ctx context.Context, userID string, yearMonths []string) (map[string]pkgfinance.MonthlySummary, error)
	CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]pkgfinance.CashFlowPoint, error)
	ListCategories(ctx context.Context, userID string) ([]domain.Category, error)
	// SaveInsightSnapshot persists the daily analysis as a subproduct of the
	// digest run, so the dashboard-api can serve it without recomputing.
	SaveInsightSnapshot(ctx context.Context, userID, date string, snapshot []byte, computedAt time.Time) error
}

// WindowReader is the slice of the session store the notifier needs: it asks
// when a phone's 24h customer-service window closes, and nothing else. It never
// records an inbound message and has no business with the dedup mark, so taking
// the whole wasession.Store meant a signature that churned every time the other
// half of that store changed — as it just did (ADR-029).
//
// ActiveUntil rather than Active because the notifier has to explain a
// non-delivery: "the window shut at 14:20" and "this phone never messaged us"
// are different problems with different fixes.
type WindowReader interface {
	ActiveUntil(ctx context.Context, phone string) (time.Time, error)
}

type Notifier struct {
	store         LedgerReader
	sessions      WindowReader
	wa            whatsapp.Client
	phoneNumberID string
	dashboardURL  string
	loc           *time.Location
	now           func() time.Time
	gen           orchestrator.TextGenerator
}

// New builds a Notifier. sessions gates delivery to WhatsApp's customer-service
// window (see packages/wasession). loc is the timezone whose calendar day
// defines "today" / "vence hoje" (nil falls back to UTC). gen is the text
// generator used to personalize the daily digest (pass StaticClient{} or
// NewTextGenerator from the orchestrator package). The clock is time.Now;
// tests can override it via SetClock.
func New(store LedgerReader, sessions WindowReader, wa whatsapp.Client, phoneNumberID string, dashboardURL string, loc *time.Location, gen orchestrator.TextGenerator) *Notifier {
	if loc == nil {
		loc = time.UTC
	}
	return &Notifier{
		store:         store,
		sessions:      sessions,
		wa:            wa,
		phoneNumberID: phoneNumberID,
		dashboardURL:  dashboardURL,
		loc:           loc,
		now:           time.Now,
		gen:           gen,
	}
}

// SetClock overrides the time source (tests only).
func (n *Notifier) SetClock(now func() time.Time) { n.now = now }

// Result summarizes one run for logging/telemetry. Every counter is a distinct
// reason a message did or did not go out, because "skipped" on its own is not an
// answer to "why didn't I get a message today?" — the fixes for "you already got
// it", "your WhatsApp window is shut" and "your account has no phone number"
// have nothing in common.
type Result struct {
	Prefs int // recipient rows found
	// Unreachable is a registered user with no phone on their Cognito profile.
	// It is the only way left to not be sent to, and unlike the switch it
	// replaced it is nobody's choice — so it keeps a counter and a log line of
	// its own rather than being left to infer from Prefs and Sent disagreeing.
	Unreachable int
	Evaluated   int // recipients with a phone to send to
	Sent        int // digests actually delivered
	// BillListsSent counts the messages carrying the day's outflows, which follow
	// the digest and can be more than one per recipient on a heavy day. Counted
	// apart from Sent because they are a different message with a different
	// failure mode: a digest that arrives without its list is a real outcome, and
	// "sent=2" would hide it.
	BillListsSent      int
	SkippedAlreadySent int // today's digest already went out (dedupe hit)
	OutsideWindow      int // enabled, but WhatsApp's 24h window is closed
	Errors             int // per-user failures; the run continues past them
}

// Skipped is the total of every "evaluated but not sent, without an error"
// outcome. Kept as a derived value so the summary can still be read at a glance.
func (r Result) Skipped() int { return r.SkippedAlreadySent }

// LogAttrs renders the counters as flat log fields. It lives next to the struct
// so a new counter is named for the log in the same place it is declared —
// otherwise a field gets added and quietly never shows up in the summary anyone
// actually reads. Flat rather than grouped because the runbook greps these keys.
func (r Result) LogAttrs() []any {
	return []any{
		"prefs", r.Prefs,
		"unreachable", r.Unreachable,
		"evaluated", r.Evaluated,
		"sent", r.Sent,
		"bill_lists_sent", r.BillListsSent,
		"skipped_already_sent", r.SkippedAlreadySent,
		"outside_window", r.OutsideWindow,
		"errors", r.Errors,
	}
}

// dayRun is the run-scoped state every recipient in a run shares: the logger
// already bound to the date and the kind, the delivery-log key for this run, and
// the instant the run started. They travel together because they are decided
// together, once, in Run — and because passing them one by one is how a function
// ends up with seven positional parameters.
type dayRun struct {
	log       *slog.Logger
	dedupeKey string
	now       time.Time
}

// RunKind names which of the day's scheduled runs this is. The two do
// different work — see Run — and the caller says which rather than the Lambda
// guessing from the hour on its own clock: a schedule that drifts, or a retry
// that lands late, must not turn the afternoon reminder into a second morning
// digest.
type RunKind string

const (
	// RunDigest is the morning run: the day's message, plus the list of what is
	// due, plus the analysis snapshot the dashboard serves.
	RunDigest RunKind = "digest"
	// RunOpenBills is the afternoon run: what is left of the day's outflows —
	// the ones still unpaid, or the sentence saying there are none. It computes
	// no analysis, writes no snapshot and rewrites nothing through the model.
	RunOpenBills RunKind = "saidas_tarde"
)

// ParseRunKind resolves the scheduler's `run` field. An empty value is the
// morning digest, so a schedule with no input configured keeps working; an
// unrecognised one is an error rather than a default, because the failure it
// prevents — the wrong message going out to real people at the wrong hour — is
// worse than one run erroring loudly on its first fire after a deploy.
func ParseRunKind(s string) (RunKind, error) {
	switch RunKind(s) {
	case "", RunDigest:
		return RunDigest, nil
	case RunOpenBills:
		return RunOpenBills, nil
	default:
		return "", fmt.Errorf("unknown notifier run kind %q (want %q or %q)", s, RunDigest, RunOpenBills)
	}
}

// Run performs one scheduled run of the given kind. It keeps going past a
// per-user failure and returns the joined errors, so one bad user never blocks
// the rest.
//
// Every user gets exactly one log line naming the outcome and, when nothing was
// sent, the reason. This runs twice a day for a handful of recipients, so the
// volume is negligible against CloudWatch's free tier — and the alternative was
// a summary saying "skipped=1" with no way to find out who, or why, after the
// fact. A silent day has to be diagnosable from the logs alone.
//
// The preamble below is what the two runs share: the day, who is opted in, and
// the ledger. Everything after the split is specific to one of them.
func (n *Notifier) Run(ctx context.Context, kind RunKind) (Result, error) {
	var res Result

	nowInstant := n.now()
	nowT := nowInstant.In(n.loc)
	y, m, d := nowT.Date()
	// Anchor everything to a UTC calendar date so comparisons line up with
	// how entries store their (timezone-free) dates.
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	month := domain.MonthOf(today)
	windowStart := time.Date(y, m-OverdueLookbackMonths, 1, 0, 0, 0, 0, time.UTC)
	dedupeKey := today.Format("2006-01-02")

	// Every line this run emits carries the date, and every per-user line also
	// carries who it is about. Binding those once with With beats repeating them
	// at each call site: they cannot drift apart, and a new log line gets them
	// for free instead of by remembering to.
	runLog := slog.With("date", dedupeKey, "run", string(kind))
	runLog.Info("notifier run started", "timezone", n.loc.String())

	// The afternoon reminder keys its own delivery log: it is a second message
	// on the same day, and sharing the morning's key would have each of them
	// silence the other.
	if kind == RunOpenBills {
		dedupeKey += "#saidas-tarde"
	}

	prefsList, err := n.store.ListNotificationPrefs(ctx)
	if err != nil {
		err = fmt.Errorf("list notification prefs: %w", err)
		runLog.Error("notifier run aborted", "error", err)
		return res, err
	}
	res.Prefs = len(prefsList)

	// userLog is the run logger plus this recipient's identity.
	userLog := func(prefs domain.NotificationPrefs) *slog.Logger {
		return runLog.With("user", prefs.UserID, "phone", maskPhone(prefs.Phone))
	}

	var errs []error
	fail := func(log *slog.Logger, err error) {
		res.Errors++
		log.Error("notifier digest failed", "error", err)
		errs = append(errs, err)
	}

	// Every prefs row names a real Cognito user (who to notify, and on which
	// phone), but they all read the same shared financial ledger — filter down
	// to reachable recipients first so a fresh install with nobody registered
	// skips the ledger reads below entirely.
	var candidates []domain.NotificationPrefs
	for _, prefs := range prefsList {
		if prefs.Phone == "" {
			res.Unreachable++
			userLog(prefs).Info("notifier message not sent",
				"reason", "no_phone",
				"detail", "no phone number on this user's Cognito profile")
			continue
		}
		candidates = append(candidates, prefs)
	}
	if len(candidates) == 0 {
		runLog.Warn("notifier run finished with no eligible recipients",
			"prefs", res.Prefs, "unreachable", res.Unreachable)
		return res, nil
	}

	// One ledger, read once — reused for every recipient below instead of
	// once per recipient.
	entries, err := n.store.ListEntries(ctx, shared.FinanceLedgerID, pkgfinance.EntryFilter{
		From: &windowStart,
		To:   &today,
	})
	if err != nil {
		err = fmt.Errorf("list entries: %w", err)
		runLog.Error("notifier run aborted", "error", err)
		return res, err
	}
	// What the ledger's unpaid bills look like today, independent of anyone's
	// preferences — see notifications.BillStatus. The alerts below say what each
	// user asked to hear; this says what is actually there, which is what a
	// message claiming the day is quiet has to be built on.
	bills := notifications.Bills(entries, today)
	// The same bills, one by one, for the list that goes out behind the digest
	// and again in the afternoon. Read here with everything else the runs share:
	// one ledger, one pass.
	dueBills := notifications.DueToday(entries, today)

	// The afternoon run's whole job is those bills. It stops here — no goal, no
	// summary, no analysis, no snapshot: none of it would reach a message, and
	// recomputing the analysis at 15h would also overwrite the morning's
	// snapshot with a second, unnecessary write.
	if kind == RunOpenBills {
		err := n.remindOpenBills(ctx, dayRun{log: runLog, dedupeKey: dedupeKey, now: nowInstant}, &res, candidates, dueBills)
		return res, err
	}

	// A missing goal is fine — Evaluate treats a zero target as "no goal".
	goal, _ := n.store.GetGoal(ctx, shared.FinanceLedgerID, month)
	// The goal alert compares against *this month's* faturamento, which cannot
	// be derived from `entries` above: that slice spans the overdue lookback
	// window, several months wide, and is bucketed by effective date, so a
	// crediário sale made this month but due next is missing from it entirely.
	// Read the month's summary instead — TotalRevenue is sales only, on the
	// transaction basis, which is exactly what a sales target is set against.
	var revenue int64
	if summaries, serr := n.store.MultiMonthlySummary(ctx, shared.FinanceLedgerID, []string{month}); serr != nil {
		// Without the month's faturamento the goal alert cannot be evaluated
		// honestly. Skipping it costs one notification; guessing from the
		// lookback window would announce "meta atingida" off the wrong number.
		runLog.Warn("monthly summary unavailable, skipping goal alert", "error", serr)
	} else {
		revenue = summaries[month].TotalRevenue
	}

	// The month's analysis, read once for the whole run like the ledger above.
	// It is context for the alerts, not a reason to send: a failure here costs
	// the digest its "how is the month going" section and nothing more, so it
	// is logged and stepped over rather than aborting the run.
	var digestInsights, digestAhead []string
	var digestPayload map[string]any
	var commitments analytics.CommitmentCoverage
	analysis, err := analytics.Assemble(ctx, n.store, shared.FinanceLedgerID, month, nowT)
	if err != nil {
		runLog.Warn("notifier digest analysis unavailable, sending alerts alone", "error", err)
	} else {
		digestInsights = analysis.DigestLines()
		digestAhead = analysis.AheadLines()
		// The whole insights JSON — the same one the analysis page renders and
		// the bot reads — is what the model writes the message from. It used to
		// receive the rendered lines alone, which is why the digest could only
		// ever say what those lines already said.
		digestPayload = analysis.DigestPayload()
		commitments = analysis.CashPosition.Commitments

		// Persist the analysis as a daily snapshot — subproduct of the digest
		// run, zero extra calculation. The dashboard-api serves this instead of
		// recomputing on every request.
		if snapshotJSON, merr := json.Marshal(analysis); merr != nil {
			runLog.Warn("marshal analysis snapshot", "error", merr)
		} else if serr := n.store.SaveInsightSnapshot(ctx, shared.FinanceLedgerID, today.Format("2006-01-02"), snapshotJSON, nowInstant); serr != nil {
			runLog.Warn("persist analysis snapshot", "error", serr)
		}
	}

	for _, prefs := range candidates {
		res.Evaluated++
		log := userLog(prefs)

		// WhatsApp only lets us send free-form messages within its
		// customer-service window (see packages/wasession). Outside it we'd need
		// a paid template, so we stay silent instead. Checked before any other
		// work so out-of-window users cost just one GetItem.
		expiry, err := n.sessions.ActiveUntil(ctx, prefs.Phone)
		if err != nil {
			fail(log, fmt.Errorf("user %s: session check: %w", prefs.UserID, err))
			continue
		}
		if expiry.IsZero() || !expiry.After(nowInstant) {
			res.OutsideWindow++
			// Distinguish "never wrote to us" from "wrote, but too long ago":
			// the first needs any message at all, the second needs one before
			// tomorrow's run. Both are fixed by the user texting the bot, so the
			// log says exactly how stale the session is.
			if expiry.IsZero() {
				log.Info("notifier digest not sent",
					"reason", "outside_whatsapp_window",
					"detail", "no session record — this phone has not messaged the bot recently")
			} else {
				log.Info("notifier digest not sent",
					"reason", "outside_whatsapp_window",
					"window_closed_at", expiry.In(n.loc).Format(time.RFC3339),
					"closed_for", nowInstant.Sub(expiry).Round(time.Minute).String())
			}
			continue
		}

		alerts := notifications.Evaluate(entries, revenue, goal, today)

		already, err := n.store.NotificationSent(ctx, prefs.UserID, dedupeKey)
		if err != nil {
			fail(log, fmt.Errorf("user %s: check log: %w", prefs.UserID, err))
			continue
		}
		if already {
			res.SkippedAlreadySent++
			log.Info("notifier digest not sent", "reason", "already_sent_today", "alerts", len(alerts))
			continue
		}

		// The digest goes out every day, quiet ones included: a morning with
		// nothing to flag is an answer to "como estamos?", and a silence is not —
		// it reads the same as a broken cron. No branch can hold it back now, and
		// none can leave it empty either: a day with bills has alerts, and a day
		// without them has the calm line.
		listFollows := len(dueBills) > 0
		content := digestContent{
			Calm:        calmLine(bills, commitments),
			Alerts:      alerts,
			Ahead:       digestAhead,
			Insights:    digestInsights,
			Payload:     digestPayload,
			ListFollows: listFollows,
		}

		msg := n.buildDigest(ctx, content)
		if err := n.wa.SendText(ctx, n.phoneNumberID, prefs.Phone, msg); err != nil {
			fail(log, fmt.Errorf("user %s: send: %w", prefs.UserID, err))
			continue
		}
		res.Sent++
		log.Info("notifier digest sent",
			"alerts", len(alerts), "kinds", alertKinds(alerts),
			"calm_day", content.Calm != "", "chars", len(msg))

		// The day's outflows follow as their own message, verbatim and complete —
		// see billListMessages. Only for a recipient who hears about bills due
		// today: the list is the detail behind that alert, and sending it to
		// someone who switched the alert off would be reporting the kind through
		// the back door.
		if listFollows {
			parts := billListMessages(dueBills, dueTodayTitle)
			sent, err := n.sendAll(ctx, prefs.Phone, parts)
			res.BillListsSent += sent
			if err != nil {
				// Not a `continue`: the digest did go out, and the delivery log
				// below has to record that or tomorrow's run repeats it. The
				// bills themselves get a second chance at 15h, from the afternoon
				// run's own key.
				fail(log, fmt.Errorf("user %s: send bill list: %w", prefs.UserID, err))
			} else {
				log.Info("notifier bill list sent", "bills", len(dueBills), "messages", len(parts))
			}
		}

		// Record only after a successful send. A failure here risks a resend
		// tomorrow, which is far better than dropping the alert entirely.
		if err := n.store.RecordNotificationSent(ctx, prefs.UserID, dedupeKey, n.now()); err != nil {
			fail(log, fmt.Errorf("user %s: record log: %w", prefs.UserID, err))
		}
	}

	// One line carrying every outcome, so a silent day is explained by the
	// summary alone and the per-user lines are only needed to name names.
	if res.Sent == 0 {
		// Nobody heard from us today. That is sometimes correct (nothing to
		// report) and sometimes a bug, and either way it is the run worth
		// finding in the logs.
		runLog.Warn("notifier run finished without sending anything", res.LogAttrs()...)
	} else {
		runLog.Info("notifier run finished", res.LogAttrs()...)
	}
	return res, errors.Join(errs...)
}

// maskPhone keeps a phone recognisable in logs without writing the whole number
// down: enough to match against a user, not enough to be a contact list.
func maskPhone(phone string) string {
	if len(phone) <= 4 {
		return "****"
	}
	return "****" + phone[len(phone)-4:]
}

// alertKinds lists which rules fired, so a digest that looks wrong can be traced
// to a rule without re-running the evaluation.
func alertKinds(alerts []notifications.Alert) string {
	kinds := make([]string, 0, len(alerts))
	for _, a := range alerts {
		kinds = append(kinds, string(a.Kind))
	}
	return strings.Join(kinds, ",")
}

// digestContent is everything one recipient's message can be built from: the
// deterministic draft's parts, and the analysis JSON the model writes from.
//
// The three line groups are separate because they are about different times and
// the message keeps them apart — see buildAlertsBody.
type digestContent struct {
	// Calm is the "nothing is asking for you today" line, present only when the
	// day is verifiably quiet for the kinds this recipient hears about.
	Calm     string
	Alerts   []notifications.Alert
	Ahead    []string
	Insights []string
	// Payload is the insights JSON — nil when the month's analysis could not be
	// assembled, in which case the model writes from the draft alone.
	Payload map[string]any
	// ListFollows says a second message with the day's outflows is right behind
	// this one. The model is told, so it stays on the total instead of writing
	// out a list that is about to arrive in full — and better — underneath it.
	ListFollows bool
}

// calmLine states that the day is quiet, and says nothing at all when it is
// not: a bill that needs attention has an alert of its own two lines down, and
// a ✅ above it would be the message disagreeing with itself.
//
// The claim comes from the ledger (notifications.BillStatus) rather than from an
// empty alert list. Those two agree now that nothing filters the alerts — but
// they agree because of how Evaluate happens to be written today, and this is
// the sentence in the message that has to be true whatever anyone does to that
// function next.
func calmLine(bills notifications.BillStatus, commitments analytics.CommitmentCoverage) string {
	if !bills.Quiet() {
		return ""
	}

	line := "✅ Nada vence hoje e não há contas vencidas."
	// The cash clause is added only where the projected balance says so. An
	// uncovered month has its own line in the analysis half, and a ledger with no
	// trading history cannot answer the question at all — neither may be dressed
	// up as calm here (ADR-022).
	if commitments == analytics.CommitmentsCovered {
		line += " O caixa projetado cobre os compromissos do mês."
	}
	return line
}

// remindOpenBills is the afternoon run: the day's outflows again, minus the
// ones that have been paid since the morning.
//
// It is a reminder and not a repeat, and the difference is the ledger's: the
// list is rebuilt from what is *still* pending, so a bill settled at eleven is
// gone from the three o'clock message. That is also why it is worth sending at
// all — a morning list is read before the day starts and forgotten by the
// afternoon, and the shorter the second one is, the more it is worth reading.
//
// It goes out every day, including the days with nothing in it. That is the
// same rule the digest follows (ADR-023) and it was asked for by the pharmacy
// itself: a message that only arrives on busy days is one whose absence has to
// be interpreted, and "nada vence hoje" is exactly the thing nobody wants to
// find out by guessing.
//
// Everything the digest run does beyond this is deliberately absent. No model
// rewrites it (see billListMessages), no analysis is assembled and no snapshot
// is written: the afternoon has nothing new to say about the month, and a
// second snapshot a day would only overwrite the morning's with the same
// figures.
func (n *Notifier) remindOpenBills(ctx context.Context, run dayRun, res *Result, candidates []domain.NotificationPrefs, dueBills []notifications.Bill) error {
	var errs []error
	fail := func(log *slog.Logger, err error) {
		res.Errors++
		log.Error("notifier bill reminder failed", "error", err)
		errs = append(errs, err)
	}

	for _, prefs := range candidates {
		res.Evaluated++
		log := run.log.With("user", prefs.UserID, "phone", maskPhone(prefs.Phone))

		expiry, err := n.sessions.ActiveUntil(ctx, prefs.Phone)
		if err != nil {
			fail(log, fmt.Errorf("user %s: session check: %w", prefs.UserID, err))
			continue
		}
		if expiry.IsZero() || !expiry.After(run.now) {
			res.OutsideWindow++
			log.Info("notifier bill reminder not sent", "reason", "outside_whatsapp_window")
			continue
		}

		already, err := n.store.NotificationSent(ctx, prefs.UserID, run.dedupeKey)
		if err != nil {
			fail(log, fmt.Errorf("user %s: check log: %w", prefs.UserID, err))
			continue
		}
		if already {
			res.SkippedAlreadySent++
			log.Info("notifier bill reminder not sent", "reason", "already_sent_today")
			continue
		}

		// One question — "o que ainda tenho para pagar hoje?" — and two answers:
		// the list, or the line saying there is nothing on it. Never silence: a
		// reminder that did not arrive and a day that needed none look exactly
		// alike on a phone, and only one of them is a working schedule.
		parts := billListMessages(dueBills, openBillsTitle)
		if len(parts) == 0 {
			parts = []string{noOpenBillsMessage}
		}

		sent, err := n.sendAll(ctx, prefs.Phone, parts)
		res.BillListsSent += sent
		if err != nil {
			// Nothing recorded, so the scheduler's retry — or tomorrow — can try
			// again. There is no half-sent state worth protecting here: the
			// reminder is one list, and a resend of it is harmless.
			fail(log, fmt.Errorf("user %s: send bill reminder: %w", prefs.UserID, err))
			continue
		}
		log.Info("notifier bill reminder sent", "open", len(dueBills), "messages", len(parts))

		if err := n.store.RecordNotificationSent(ctx, prefs.UserID, run.dedupeKey, n.now()); err != nil {
			fail(log, fmt.Errorf("user %s: record log: %w", prefs.UserID, err))
		}
	}

	if res.BillListsSent == 0 {
		run.log.Warn("notifier run finished without sending anything", res.LogAttrs()...)
	} else {
		run.log.Info("notifier run finished", res.LogAttrs()...)
	}
	return errors.Join(errs...)
}

// sendAll delivers a message and its continuations in order, returning how many
// went out before the first failure. It stops there rather than pressing on: the
// parts are one list cut into pieces, and a gap in the middle of it is harder to
// read than a list that plainly stops.
func (n *Notifier) sendAll(ctx context.Context, phone string, parts []string) (int, error) {
	for i, part := range parts {
		if err := n.wa.SendText(ctx, n.phoneNumberID, phone, part); err != nil {
			return i, fmt.Errorf("part %d/%d: %w", i+1, len(parts), err)
		}
	}
	return len(parts), nil
}

// noOpenBillsMessage is the afternoon of a day with nothing left to pay. It
// covers both ways a day gets there — bills that were all settled, and a day
// that never had any — because the message is about what is *pending*, and
// pending is empty either way.
const noOpenBillsMessage = "✅ *Saídas de hoje* — nenhum compromisso pendente."

// maxWhatsAppText is the budget one outgoing message gets. Meta's limit for a
// text body is 4096 characters and it rejects the whole message past it, so the
// margin here is for the header a continuation carries and for a multi-byte
// character landing on the boundary.
const maxWhatsAppText = 3800

// billListMessages renders the day's outflows as their own message — or as
// several, when one will not hold them.
//
// They are a separate message from the digest, and deliberately not part of it.
// A digest is a paragraph someone reads in the morning; this is the list they
// pay from, and the two fail differently when they are crowded together. The
// digest is rewritten by the model, which is exactly where a line goes missing
// or two amounts get merged into "cerca de R$ 3 mil" — and even without a model,
// a fifteen-bill list inside the digest pushes the rest of it past what a phone
// shows without scrolling. So the list ships verbatim: no model, no cap, no
// "e mais 4" (ADR-015), and it splits into further messages rather than losing
// its tail.
//
// Every message it returns fits the budget. That is not a detail: Meta rejects
// an over-length body whole, and the caller stops sending at the first refusal —
// so one pathological line would take every bill behind it down with it, on both
// of the day's runs, and the reader would get a digest quoting a total with
// nothing itemising it.
//
// Returns nil when nothing is due, which is not a case worth a message: the
// digest already says the day is clear.
func billListMessages(bills []notifications.Bill, title string) []string {
	if len(bills) == 0 {
		return nil
	}

	var total int64
	for _, b := range bills {
		total += b.Amount
	}
	header := fmt.Sprintf("💸 *%s* (%d) — total R$ %s",
		title, len(bills), notifications.FormatBRL(total))
	// Every chunk is measured against the same budget, so the reserve is the
	// longest header any of them could carry — which is the first one on a day
	// with a big total, and the continuation on a day with a small one. The part
	// numbers are sized at their widest rather than counted, because the count is
	// not known until the lines have been chunked against this very budget.
	reserve := max(len(header), len(continuationHeader(title, 999, 999))) + len("\n\n")
	budget := maxWhatsAppText - reserve

	lines := make([]string, 0, len(bills))
	for _, b := range bills {
		lines = append(lines, clipLine("• "+b.Text, budget))
	}

	chunks := chunkLines(lines, budget)
	messages := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		head := header
		if i > 0 {
			head = continuationHeader(title, i+1, len(chunks))
		}
		messages = append(messages, head+"\n\n"+strings.Join(chunk, "\n"))
	}
	return messages
}

func continuationHeader(title string, part, of int) string {
	return fmt.Sprintf("💸 *%s* — continuação (%d/%d)", title, part, of)
}

// The two titles the list ships under. They differ because the messages answer
// different questions: the morning one is the day's plan, the afternoon one is
// what is left of it — and a reader who paid three bills after lunch has to be
// able to tell why the second list is shorter without counting.
const (
	dueTodayTitle  = "Saídas previstas para hoje"
	openBillsTitle = "Saídas de hoje ainda em aberto"
)

// clipLine shortens a single line that could not fit a message on its own, and
// marks where it was cut.
//
// A bill's description is still not ours to shorten, and nothing here does it
// for tidiness: the budget is thousands of characters, so a line reaching it is
// a data-entry accident rather than a long supplier name. What changed is the
// alternative. An over-length line does not merely fail its own message — Meta
// rejects it, the send loop stops there, and the rest of the day's bills go with
// it. Losing the tail of one description is the smaller loss, it is visible (…),
// and the amount leads the line, so the money is never what gets cut.
func clipLine(line string, budget int) string {
	if budget <= 0 || len(line) <= budget {
		return line
	}
	const ellipsis = "…"
	keep := budget - len(ellipsis)
	// Back off to a rune boundary: cutting mid-sequence would ship replacement
	// characters, which is a worse way to say "this was cut" than saying it.
	for keep > 0 && !utf8.RuneStart(line[keep]) {
		keep--
	}
	return line[:keep] + ellipsis
}

// chunkLines groups lines into runs that fit the budget, counting the newline
// each one costs. Its caller has already clipped any line that could not fit one
// on its own (see clipLine), so no chunk it returns can exceed the budget.
func chunkLines(lines []string, budget int) [][]string {
	if len(lines) == 0 {
		return nil
	}
	var chunks [][]string
	var current []string
	size := 0
	for _, line := range lines {
		cost := len(line) + 1
		if len(current) > 0 && size+cost > budget {
			chunks = append(chunks, current)
			current, size = nil, 0
		}
		current = append(current, line)
		size += cost
	}
	return append(chunks, current)
}

// buildDigest renders the daily message. Only the body is handed to the model to
// rewrite; the dashboard call-to-action is appended afterwards, so the link that
// actually ships is always the configured URL and never something the model
// paraphrased, dropped or invented.
func (n *Notifier) buildDigest(ctx context.Context, content digestContent) string {
	body := buildAlertsBody(content)
	if humanized, ok := n.humanize(ctx, body, content); ok {
		body = humanized
	}
	return withDashboardLink(body, n.dashboardURL)
}

// humanize asks the model to write the day's message from the analysis. It
// reports false on any failure (or empty output) so the caller keeps the static
// draft — a broken generator must never swallow the alert.
//
// The draft goes in as the user message and the insights JSON rides in the
// system prompt, which is not an arbitrary split. NewDigestGenerator falls back
// to an echo generator when Gemini is not configured, and an echo returns the
// user message verbatim: keeping the ready-to-send draft there is what makes an
// unconfigured notifier ship the static message instead of a page of JSON.
func (n *Notifier) humanize(ctx context.Context, body string, content digestContent) (string, bool) {
	// Derived from the run's context, not from Background: a Lambda being shut
	// down should cancel the call it is waiting on, not hold the timeout open.
	genCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	output, err := n.gen.Generate(genCtx, orchestrator.Input{
		UserMessage: domain.Message{
			UserID:    "system",
			Text:      body,
			Timestamp: time.Now().UTC(),
			MessageID: "notifier-digest",
		},
		SystemPrompt: digestSystemPrompt(content),
	})
	if err != nil {
		// Not fatal: the caller ships the static draft instead. Logged all the
		// same, because a digest that silently stops being humanized looks like
		// a prompt regression from the outside.
		slog.Warn("notifier digest humanization failed, using static draft", "error", err)
		return "", false
	}
	text := stripInventedLinks(output.Text)
	if text == "" {
		return "", false
	}
	// Meta rejects an over-length body whole, so a model that runs long would
	// cost the digest entirely rather than its last paragraph. The draft is
	// bounded by construction (a handful of rendered lines); the rewrite is only
	// bounded by the generator's token limit, which is close enough to this one
	// to be worth checking.
	if len(text)+len(dashboardLink(n.dashboardURL)) > maxWhatsAppText {
		slog.Warn("notifier digest rewrite too long for WhatsApp, using static draft", "chars", len(text))
		return "", false
	}
	return text, true
}

// digestSystemPrompt is the instruction the digest is written under, with the
// month's insights JSON appended as the data of record.
//
// The JSON is the same one the analysis page renders (analytics.DigestPayload),
// and handing over the whole of it is the point: the message used to be a
// rewrite of a handful of rendered lines, so anything the analysis knew but did
// not print — the projected balance, the month's commitments and whether they
// are covered, the week's pace — could not reach the reader no matter what the
// day looked like.
//
// Which is also why the rules below are about arithmetic rather than about
// tone. Every figure in that JSON is already computed, in reais, by code with
// tests behind it; the one thing that turns it into a wrong number is the model
// combining two of them. So: quote, never compute — the same prohibition
// ADR-019 puts on the per-day target, stated once for every figure.
func digestSystemPrompt(content digestContent) string {
	prompt := "Você é um assistente financeiro que envia um resumo diário via WhatsApp. " +
		"Escreva a mensagem do dia em português, amigável e objetiva. " +
		"Mantenha o tom profissional mas acolhedor. Use emojis com moderação. " +
		"Não invente informações. " +
		// The draft is already split into "a partir de agora" and "até
		// ontem", and the split is the point: the message arrives de manhã,
		// quando o dia de hoje ainda não aconteceu.
		"O rascunho vem dividido em duas partes: o que ainda precisa ser feito " +
		"a partir de agora e como o mês fechou até ontem. Preserve essa " +
		"divisão e nunca apresente um número do passado como se fosse de hoje. " +
		// The model turned "Faturamento caiu — 100% abaixo do mês passado
		// (até o dia 1)" into a bare "queda de 100% em relação ao mês
		// passado", which reads as the whole month having collapsed.
		"Se uma linha disser \"até o dia N\" ou \"até ontem\", repita essa " +
		"ressalva junto do número — sem ela a comparação vira um mês inteiro. " +
		"Se o rascunho disser que o mês está começando, não invente comparação " +
		"nem diagnóstico: fale só do que vem pela frente. " +
		// Days 2–7 have closed days to report on but no comparable window:
		// the 1st and 2nd of one month are not the same weekdays as the 1st
		// and 2nd of the one before.
		"Se o rascunho disser que a primeira semana ainda não fechou, repita " +
		"isso e não compare o mês com o anterior de nenhuma outra forma. " +
		// The draft used to carry no daily figure at all, so the model
		// filled the gap with the gap-over-days-left division — a flat ask
		// that reads the same on a Sunday as on a Saturday (ADR-019).
		"Nunca calcule uma meta por dia dividindo valores: se houver meta " +
		"para hoje, ela já vem no rascunho, junto do que aquele dia da " +
		"semana costuma faturar. Repita os dois. " +
		// The digest now goes out on quiet days too. A model handed an analysis
		// and asked for a message will find something in it to worry about if
		// nobody says otherwise — and a daily warning about a month that is fine
		// is how a reader learns to stop opening the message.
		"Um dia tranquilo é uma boa notícia e a mensagem deve ser curta: confirme " +
		"o que o rascunho diz que está em ordem e pare por aí. Não procure um " +
		"problema para preencher a mensagem. " +
		"IMPORTANTE: não escreva links, URLs nem textos substitutos como " +
		"\"[Link para o dashboard]\" — o link é acrescentado automaticamente " +
		"depois da sua resposta."

	// Said only when it is true. Telling the model about a message that is not
	// coming would have it referring the reader to a list nobody will receive.
	if content.ListFollows {
		prompt += " As contas que vencem hoje vão listadas uma a uma na mensagem " +
			"seguinte, logo depois desta. Fique no total: não repita os itens nem " +
			"invente quantos são."
	}

	if len(content.Payload) == 0 {
		return prompt
	}
	data, err := json.Marshal(content.Payload)
	if err != nil {
		// The draft alone is still a complete message; the JSON only makes it
		// richer. Nothing about this is worth failing the digest over.
		slog.Warn("notifier digest payload not serializable, prompting with the draft alone", "error", err)
		return prompt
	}

	return prompt + "\n\n" +
		"Os números da análise do mês vêm abaixo em JSON — é exatamente o que a " +
		"página de Análise mostra, já calculado, com os valores em reais. " +
		"Você pode citar qualquer número que esteja nele ou no rascunho, e pode " +
		"usá-lo para dar contexto ao que o rascunho já diz. " +
		"Nunca some, subtraia, divida ou calcule um número novo a partir deles: " +
		"se o número não está escrito, ele não existe. " +
		"Nem tudo que está no JSON precisa entrar na mensagem — escolha o que " +
		"ajuda quem lê às sete da manhã e ignore o resto.\n\n" +
		string(data)
}

// linkPlaceholderRE matches the fill-in-the-blank links models emit when asked
// to include a URL they were never given — "[Link para o dashboard]",
// "[inserir link aqui]", "[URL do painel]".
var linkPlaceholderRE = regexp.MustCompile(`(?i)\[[^\]\n]*(link|url|dashboard|painel)[^\]\n]*\]`)

// markdownLinkRE matches "[rótulo](https://…)". WhatsApp has no markdown, so a
// real URL wrapped this way would ship with its brackets showing.
var markdownLinkRE = regexp.MustCompile(`\[[^\]\n]*\]\((https?://[^)\s]+)\)`)

// stripInventedLinks removes link artifacts from generated copy. Markdown links
// keep their URL and lose the brackets; a line carrying a bare placeholder is
// dropped whole, since it is by construction the call-to-action sentence that
// withDashboardLink re-adds with the real URL.
func stripInventedLinks(s string) string {
	s = markdownLinkRE.ReplaceAllString(s, "$1")

	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if linkPlaceholderRE.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// withDashboardLink appends the dashboard call-to-action. A body that already
// carries the URL (the static draft path) is returned untouched, and an
// unconfigured DASHBOARD_URL simply yields no call-to-action rather than a
// dangling "acesse aqui:".
func withDashboardLink(body, dashboardURL string) string {
	link := dashboardLink(dashboardURL)
	if link == "" || strings.Contains(body, link) {
		return body
	}
	return body + "\n\n" + link
}

func dashboardLink(dashboardURL string) string {
	if dashboardURL == "" {
		return ""
	}
	return "📊 Acesse a análise completa: " + strings.TrimRight(dashboardURL, "/") + "/analise"
}

// buildAlertsBody is the static draft: the message we send verbatim when there
// is no model to rewrite it, and the input the model rewrites when there is.
//
// It is written in the order the day is actually lived. What has to be dealt
// with from now on comes first — the bills with a deadline, then what the days
// left have to bring — because that is what the reader can still act on when
// the digest lands in the morning. How the month has gone follows, and it is
// explicitly labelled as being about the days that have *finished*: the digest
// used to lead with a verdict on a day nobody had traded yet, and on the 1st it
// opened with "saúde crítica" and "receita caiu 100%" against a month whose
// only content was its own bills.
// It is also the message a quiet day gets, and that is not a lesser case: the
// digest used to go out only when a rule fired, so "nothing needs you today"
// arrived as silence — indistinguishable from a broken schedule, and the one
// morning report a pharmacist can act on by *not* acting. The calm line leads
// the same "a partir de agora" half the alerts do, because it is an answer to
// the same question.
func buildAlertsBody(content digestContent) string {
	var b strings.Builder
	b.WriteString("🔔 *Farmácia Financeira* — resumo do dia\n")

	if content.Calm != "" || len(content.Alerts) > 0 || len(content.Ahead) > 0 {
		b.WriteString("\n⏭️ *A partir de agora:*\n")
		if content.Calm != "" {
			b.WriteString("\n• ")
			b.WriteString(content.Calm)
		}
		for _, a := range content.Alerts {
			b.WriteString("\n• ")
			b.WriteString(a.Text)
		}
		for _, line := range content.Ahead {
			b.WriteString("\n• ")
			b.WriteString(line)
		}
	}

	if insights := content.Insights; len(insights) > 0 {
		b.WriteString("\n\n📊 *Como fechamos até ontem:*\n")
		for _, line := range insights {
			b.WriteString("\n• ")
			b.WriteString(line)
		}
	}
	return b.String()
}
