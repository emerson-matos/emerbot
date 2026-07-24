// Package notifier evaluates each user's alerts once and sends a single daily
// WhatsApp digest, deduplicated per day. It is the scheduled (EventBridge) twin
// of the dashboard's notification bell: same rules (via packages/notifications),
// different delivery channel.
package notifier

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/notifications"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
	"github.com/emerson/emerbot/packages/whatsapp"
)

// OverdueLookbackMonths bounds how far back the entries query reaches when
// looking for still-pending bills — matches the web hook's window.
const OverdueLookbackMonths = 3

type Notifier struct {
	store         pkgfinance.Store
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
func New(store pkgfinance.Store, sessions wasession.Store, wa whatsapp.Client, phoneNumberID string, dashboardURL string, loc *time.Location, gen orchestrator.TextGenerator) *Notifier {
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

// Result summarizes one run for logging/telemetry.
type Result struct {
	Evaluated     int // users with WhatsApp enabled + a phone
	Sent          int // digests actually delivered
	Skipped       int // no alerts, or already sent today
	OutsideWindow int // enabled, but no inbound message in the last 24h
}

// Run evaluates every enabled user and sends at most one digest each. It keeps
// going past a per-user failure and returns the joined errors, so one bad user
// never blocks the rest.
//
// Logging is deliberately terse (one line per error, one summary line) — this
// runs once a day, so even generous logging is nowhere near CloudWatch Logs'
// free tier, but there's no reason to pay for what we don't need either.
func (n *Notifier) Run(ctx context.Context) (Result, error) {
	var res Result

	nowInstant := n.now()
	nowT := nowInstant.In(n.loc)
	y, m, d := nowT.Date()
	// Anchor everything to a UTC calendar date so comparisons line up with
	// how entries store their (timezone-free) dates.
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	month := today.Format("2006-01")
	windowStart := time.Date(y, m-OverdueLookbackMonths, 1, 0, 0, 0, 0, time.UTC)
	dedupeKey := today.Format("2006-01-02")

	log.Printf("notifier: level=info msg=run_started date=%s", dedupeKey)

	prefsList, err := n.store.ListNotificationPrefs(ctx)
	if err != nil {
		err = fmt.Errorf("list notification prefs: %w", err)
		log.Printf("notifier: level=error msg=%q", err)
		return res, err
	}

	var errs []error
	fail := func(err error) {
		log.Printf("notifier: level=error msg=%q", err)
		errs = append(errs, err)
	}

	// Every prefs row names a real Cognito user (who to notify, and on which
	// phone), but they all read the same shared financial ledger — filter down
	// to opted-in recipients first so a fresh install with nobody enabled
	// skips the ledger reads below entirely.
	var candidates []domain.NotificationPrefs
	for _, prefs := range prefsList {
		if prefs.WAEnabled && prefs.Phone != "" {
			candidates = append(candidates, prefs)
		}
	}
	if len(candidates) == 0 {
		log.Printf("notifier: level=info msg=run_finished date=%s evaluated=0 sent=0 skipped=0 outside_window=0 errors=0", dedupeKey)
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
		log.Printf("notifier: level=error msg=%q", err)
		return res, err
	}
	// A missing goal is fine — Evaluate treats a zero target as "no goal".
	goal, _ := n.store.GetGoal(ctx, shared.FinanceLedgerID, month)
	vbIncome := pkgfinance.VendaBalcaoIncome(entries)

	for _, prefs := range candidates {
		res.Evaluated++

		// WhatsApp only lets us send free-form messages within its
		// customer-service window (see packages/wasession). Outside it we'd need
		// a paid template, so we stay silent instead. Checked before any other
		// work so out-of-window users cost just one GetItem.
		active, err := n.sessions.Active(ctx, prefs.Phone, nowInstant)
		if err != nil {
			fail(fmt.Errorf("user %s: session check: %w", prefs.UserID, err))
			continue
		}
		if !active {
			res.OutsideWindow++
			continue
		}

		alerts := notifications.Evaluate(prefs, entries, vbIncome, goal, today)
		if len(alerts) == 0 {
			res.Skipped++
			continue
		}

		already, err := n.store.NotificationSent(ctx, prefs.UserID, dedupeKey)
		if err != nil {
			fail(fmt.Errorf("user %s: check log: %w", prefs.UserID, err))
			continue
		}
		if already {
			res.Skipped++
			continue
		}

		msg := n.buildDigest(alerts)
		if err := n.wa.SendText(ctx, n.phoneNumberID, prefs.Phone, msg); err != nil {
			fail(fmt.Errorf("user %s: send: %w", prefs.UserID, err))
			continue
		}
		res.Sent++
		log.Printf("notifier: level=info msg=sent user=%s alerts=%d", prefs.UserID, len(alerts))

		// Record only after a successful send. A failure here risks a resend
		// tomorrow, which is far better than dropping the alert entirely.
		if err := n.store.RecordNotificationSent(ctx, prefs.UserID, dedupeKey, n.now()); err != nil {
			fail(fmt.Errorf("user %s: record log: %w", prefs.UserID, err))
		}
	}

	log.Printf("notifier: level=info msg=run_finished date=%s evaluated=%d sent=%d skipped=%d outside_window=%d errors=%d",
		dedupeKey, res.Evaluated, res.Sent, res.Skipped, res.OutsideWindow, len(errs))
	return res, errors.Join(errs...)
}

func (n *Notifier) buildDigest(alerts []notifications.Alert) string {
	fallback := buildStaticDigest(alerts, n.dashboardURL)

	genCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := n.gen.Generate(genCtx, orchestrator.Input{
		UserMessage: domain.Message{
			UserID:    "system",
			Text:      fallback,
			Timestamp: time.Now().UTC(),
			MessageID: "notifier-digest",
		},
		SystemPrompt: "Você é um assistente financeiro que envia um resumo diário via WhatsApp. " +
			"Transforme os alertas abaixo em uma mensagem amigável e objetiva em português. " +
			"Mantenha o tom profissional mas acolhedor. Use emojis com moderação. " +
			"Não invente informações. Se não houver alertas, diga que está tudo em ordem. " +
			"IMPORTANTE: Preserve o link para o dashboard que aparece no final da mensagem — ele é importante para o usuário.",
	})
	if err != nil || strings.TrimSpace(output.Text) == "" {
		return fallback
	}
	return strings.TrimSpace(output.Text)
}

func buildStaticDigest(alerts []notifications.Alert, dashboardURL string) string {
	var b strings.Builder
	b.WriteString("🔔 *Farmácia Financeira* — resumo de hoje:\n")
	for _, a := range alerts {
		b.WriteString("\n• ")
		b.WriteString(a.Text)
	}
	if dashboardURL != "" {
		fmt.Fprintf(&b, "\n\n📊 Acesse a análise completa: %s/analise", dashboardURL)
	}
	return b.String()
}
