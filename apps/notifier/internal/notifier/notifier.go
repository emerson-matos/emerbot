// Package notifier evaluates each user's alerts once and sends a single daily
// WhatsApp digest, deduplicated per day. It is the scheduled (EventBridge) twin
// of the dashboard's notification bell: same rules (via packages/notifications),
// different delivery channel.
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

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/finance/analytics"
	"github.com/emerson/emerbot/packages/notifications"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
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
	// summaries and the ledger's cash-flow projection.
	MultiMonthlySummary(ctx context.Context, userID string, yearMonths []string) (map[string]pkgfinance.MonthlySummary, error)
	CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]pkgfinance.CashFlowPoint, error)
	// SaveInsightSnapshot persists the daily analysis as a subproduct of the
	// digest run, so the dashboard-api can serve it without recomputing.
	SaveInsightSnapshot(ctx context.Context, userID, date string, snapshot []byte, computedAt time.Time) error
}

type Notifier struct {
	store         LedgerReader
	sessions      wasession.Store
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
func New(store LedgerReader, sessions wasession.Store, wa whatsapp.Client, phoneNumberID string, dashboardURL string, loc *time.Location, gen orchestrator.TextGenerator) *Notifier {
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
// reason a digest did or did not go out, because "skipped" on its own is not an
// answer to "why didn't I get a message today?" — the fixes for "you have no
// alerts", "you already got it" and "your WhatsApp window is shut" have nothing
// in common.
type Result struct {
	Prefs              int // notification-preference rows found
	NotOptedIn         int // rows with WhatsApp off or no phone — never evaluated
	Evaluated          int // users with WhatsApp enabled + a phone
	Sent               int // digests actually delivered
	SkippedNoAlerts    int // evaluated, but nothing worth saying today
	SkippedAlreadySent int // today's digest already went out (dedupe hit)
	OutsideWindow      int // enabled, but WhatsApp's 24h window is closed
	Errors             int // per-user failures; the run continues past them
}

// Skipped is the total of every "evaluated but not sent, without an error"
// outcome. Kept as a derived value so the summary can still be read at a glance.
func (r Result) Skipped() int { return r.SkippedNoAlerts + r.SkippedAlreadySent }

// LogAttrs renders the counters as flat log fields. It lives next to the struct
// so a new counter is named for the log in the same place it is declared —
// otherwise a field gets added and quietly never shows up in the summary anyone
// actually reads. Flat rather than grouped because the runbook greps these keys.
func (r Result) LogAttrs() []any {
	return []any{
		"prefs", r.Prefs,
		"not_opted_in", r.NotOptedIn,
		"evaluated", r.Evaluated,
		"sent", r.Sent,
		"skipped_no_alerts", r.SkippedNoAlerts,
		"skipped_already_sent", r.SkippedAlreadySent,
		"outside_window", r.OutsideWindow,
		"errors", r.Errors,
	}
}

// Run evaluates every enabled user and sends at most one digest each. It keeps
// going past a per-user failure and returns the joined errors, so one bad user
// never blocks the rest.
//
// Every user gets exactly one log line naming the outcome and, when nothing was
// sent, the reason. This runs once a day for a handful of recipients, so the
// volume is negligible against CloudWatch's free tier — and the alternative was
// a summary saying "skipped=1" with no way to find out who, or why, after the
// fact. A silent day has to be diagnosable from the logs alone.
func (n *Notifier) Run(ctx context.Context) (Result, error) {
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
	runLog := slog.With("date", dedupeKey)
	runLog.Info("notifier run started", "timezone", n.loc.String())

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
	// to opted-in recipients first so a fresh install with nobody enabled
	// skips the ledger reads below entirely.
	var candidates []domain.NotificationPrefs
	for _, prefs := range prefsList {
		if !prefs.WAEnabled || prefs.Phone == "" {
			res.NotOptedIn++
			// The two halves of this condition are different user-facing
			// problems: one is a setting they can flip, the other is a missing
			// phone on their Cognito profile.
			reason := "whatsapp_disabled"
			if prefs.Phone == "" {
				reason = "no_phone"
			}
			userLog(prefs).Info("notifier digest not sent", "reason", reason)
			continue
		}
		candidates = append(candidates, prefs)
	}
	if len(candidates) == 0 {
		runLog.Warn("notifier run finished with no eligible recipients",
			"prefs", res.Prefs, "not_opted_in", res.NotOptedIn)
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
	var digestInsights []string
	analysis, err := analytics.Assemble(ctx, n.store, shared.FinanceLedgerID, month, nowT)
	if err != nil {
		runLog.Warn("notifier digest analysis unavailable, sending alerts alone", "error", err)
	} else {
		digestInsights = analysis.DigestLines()

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

		alerts := notifications.Evaluate(prefs, entries, revenue, goal, today)
		if len(alerts) == 0 {
			res.SkippedNoAlerts++
			log.Info("notifier digest not sent",
				"reason", "no_alerts",
				"detail", "nothing met an alert rule today",
				"window_closes_at", expiry.In(n.loc).Format(time.RFC3339))
			continue
		}

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

		msg := n.buildDigest(alerts, digestInsights)
		if err := n.wa.SendText(ctx, n.phoneNumberID, prefs.Phone, msg); err != nil {
			fail(log, fmt.Errorf("user %s: send: %w", prefs.UserID, err))
			continue
		}
		res.Sent++
		log.Info("notifier digest sent",
			"alerts", len(alerts), "kinds", alertKinds(alerts), "chars", len(msg))

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

// buildDigest renders the daily message. Only the alert body is handed to the
// model to rewrite; the dashboard call-to-action is appended afterwards, so the
// link that actually ships is always the configured URL and never something the
// model paraphrased, dropped or invented.
func (n *Notifier) buildDigest(alerts []notifications.Alert, insights []string) string {
	body := buildAlertsBody(alerts, insights)
	if humanized, ok := n.humanize(body); ok {
		body = humanized
	}
	return withDashboardLink(body, n.dashboardURL)
}

// humanize asks the model to rewrite the alert body into friendlier prose. It
// reports false on any failure (or empty output) so the caller keeps the static
// draft — a broken generator must never swallow the alert.
func (n *Notifier) humanize(body string) (string, bool) {
	genCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := n.gen.Generate(genCtx, orchestrator.Input{
		UserMessage: domain.Message{
			UserID:    "system",
			Text:      body,
			Timestamp: time.Now().UTC(),
			MessageID: "notifier-digest",
		},
		SystemPrompt: "Você é um assistente financeiro que envia um resumo diário via WhatsApp. " +
			"Transforme os alertas abaixo em uma mensagem amigável e objetiva em português. " +
			"Mantenha o tom profissional mas acolhedor. Use emojis com moderação. " +
			"Não invente informações. Se não houver alertas, diga que está tudo em ordem. " +
			"IMPORTANTE: não escreva links, URLs nem textos substitutos como " +
			"\"[Link para o dashboard]\" — o link é acrescentado automaticamente " +
			"depois da sua resposta.",
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
	return text, true
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
// The alerts come first because they are the things with a deadline; the
// month's insights follow as the context that answers "and how are we doing
// overall?" without the user having to open the dashboard.
func buildAlertsBody(alerts []notifications.Alert, insights []string) string {
	var b strings.Builder
	b.WriteString("🔔 *Farmácia Financeira* — resumo de hoje:\n")
	for _, a := range alerts {
		b.WriteString("\n• ")
		b.WriteString(a.Text)
	}
	if len(insights) > 0 {
		b.WriteString("\n\n📊 *Como está o mês:*\n")
		for _, line := range insights {
			b.WriteString("\n• ")
			b.WriteString(line)
		}
	}
	return b.String()
}
