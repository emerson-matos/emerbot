package financial

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/whatsapp"
)

// LedgerStore is the slice of the finance store the slash commands use. The
// webhook records entries and goals and reads back summaries; it never touches
// categories or notification preferences, and declaring that here keeps it
// independent of methods added for the dashboard or the notifier.
type LedgerStore interface {
	SaveEntry(ctx context.Context, entry domain.FinancialEntry) error
	SaveEntries(ctx context.Context, entries []domain.FinancialEntry) error
	ListEntries(ctx context.Context, userID string, filter pkgfinance.EntryFilter) ([]domain.FinancialEntry, error)
	MonthlySummary(ctx context.Context, userID, yearMonth string) (pkgfinance.MonthlySummary, error)
	GetGoal(ctx context.Context, userID, month string) (domain.Goal, error)
	SaveGoal(ctx context.Context, goal domain.Goal) error
}

type Handler struct {
	regex *whatsapp.RegexParser
	store LedgerStore
	// loc is the calendar the pharmacy reasons about days in — "hoje",
	// "este mês" and the /resumo "a vencer amanhã" cutoff all have to agree
	// with the dashboard on what day it is. See shared.PharmacyLocation.
	loc *time.Location
}

func NewHandler(regex *whatsapp.RegexParser, store LedgerStore, loc *time.Location) *Handler {
	if loc == nil {
		loc = time.UTC
	}
	return &Handler{regex: regex, store: store, loc: loc}
}

func commandTutorial(cmd string) string {
	switch strings.ToLower(cmd) {
	case "/despesa":
		return "*/despesa <valor> <categoria> [data] [descrição]*\n" +
			"Registra uma despesa *já paga*. A data (dd/mm) é opcional — use quando a despesa não foi hoje.\n" +
			"Ex: /despesa 500 aluguel Aluguel da loja\n" +
			"Ex: /despesa 500 aluguel 10/07 Aluguel da loja\n\n" +
			"💡 Para uma despesa *ainda não paga*, use */pagar* — ela fica pendente até você quitar.\n" +
			"Ex: /pagar 300 luz 20/07"
	case "/pagar":
		return "*/pagar <valor> <categoria> [data] [descrição]*\n" +
			"Agenda uma despesa *a pagar* (fica pendente). A data de vencimento (dd/mm) é opcional.\n" +
			"Ex: /pagar 300 luz 20/07 Conta de luz"
	case "/receita":
		return "*/receita <valor> <categoria> [data] [descrição]*\n" +
			"Registra uma receita *já recebida*. A data (dd/mm) é opcional — use quando a receita não foi hoje.\n" +
			"Ex: /receita 800 venda_balcao\n" +
			"Ex: /receita 800 venda_balcao 10/07\n\n" +
			"💡 Para algo *a receber*, use */receber*."
	case "/receber":
		return "*/receber <valor> <categoria> [data] [descrição]*\n" +
			"Agenda uma receita *a receber* (fica pendente). A data (dd/mm) é opcional.\n" +
			"Ex: /receber 800 cliente_x 25/07"
	case "/meta":
		return "*/meta <faturamento> <despesa>*\n" +
			"Define as metas do mês (valores sem R$).\n" +
			"Ex: /meta 80000 60000"
	case "/recorrente":
		return "*/recorrente <pagar|receber> <valor> <categoria> <periodo> <n> [data] [descrição]*\n" +
			"Cria uma série de N lançamentos pendentes, um por período (diario, semanal, quinzenal, mensal, anual).\n" +
			"Ex: /recorrente pagar 350 aluguel mensal 12 Aluguel anual"
	default:
		return ""
	}
}

func bareCommandUsage(text string) string {
	fields := strings.Fields(text)
	if len(fields) != 1 {
		return ""
	}
	return commandTutorial(fields[0])
}

func (h *Handler) Handle(ctx context.Context, userID, text string, msgTime time.Time) (string, error) {
	if usage := bareCommandUsage(text); usage != "" {
		return usage, nil
	}

	if !strings.HasPrefix(text, "/") {
		return "", nil
	}

	parsed, err := h.regex.Parse(ctx, text, msgTime)
	if err != nil {
		return fmt.Sprintf("❌ Não consegui entender. Tente:\n/despesa 500 aluguel 10/07\n/receita 800 venda_balcao\n/pagar 300 luz 20/07\n\nErro: %s", err.Error()), nil
	}
	return h.saveAndConfirm(ctx, userID, parsed)
}

func (h *Handler) saveAndConfirm(ctx context.Context, userID string, parsed whatsapp.ParsedEntry) (string, error) {
	status := domain.PaymentStatusPaid
	if parsed.IsPending {
		status = domain.PaymentStatusPending
	}

	now := time.Now().In(h.loc)
	date := domain.NewCalendarDate(now)
	if parsed.Date != nil {
		date = domain.NewCalendarDate(*parsed.Date)
	}
	var dueDate *domain.CalendarDate
	if parsed.DueDate != nil {
		d := domain.NewCalendarDate(*parsed.DueDate)
		dueDate = &d
	}
	entry, err := domain.NewFinancialEntry(domain.NewFinancialEntryInput{
		UserID:          userID,
		TransactionDate: date,
		Amount:          parsed.Amount,
		Category:        parsed.Category,
		Type:            parsed.Type,
		Description:     parsed.Description,
		DueDate:         dueDate,
		PaymentStatus:   status,
		Source:          domain.SourceWhatsApp,
		Origin:          parsed.Origin,
	})
	if err != nil {
		return "❌ Não consegui salvar. Tente novamente.", err
	}

	if err := h.store.SaveEntry(ctx, entry); err != nil {
		return "❌ Não consegui salvar. Tente novamente.", err
	}

	return formatConfirmation(entry), nil
}

func (h *Handler) Recorrente(ctx context.Context, userID, text string) (string, error) {
	if usage := bareCommandUsage(text); usage != "" {
		return usage, nil
	}

	req, err := parseRecorrente(text, h.loc)
	if err != nil {
		return fmt.Sprintf("❌ Não consegui entender. Tente:\n/recorrente pagar 350 aluguel mensal 12 Aluguel anual\n\nErro: %s", err.Error()), nil
	}

	now := time.Now().In(h.loc)
	recurrenceID := uuid.New().String()
	entries := make([]domain.FinancialEntry, req.Occurrences)
	for i := range entries {
		due := domain.NewCalendarDate(addPeriod(req.StartDate, req.Period, i))
		entry, err := domain.NewFinancialEntry(domain.NewFinancialEntryInput{
			UserID:          userID,
			TransactionDate: domain.NewCalendarDate(now),
			Amount:          req.Amount,
			Category:        req.Category,
			Type:            req.Type,
			Description:     req.Description,
			DueDate:         &due,
			PaymentStatus:   domain.PaymentStatusPending,
			Source:          domain.SourceWhatsApp,
			Origin:          req.Origin,
			RecurrenceID:    recurrenceID,
			RecurrenceIndex: i + 1,
			RecurrenceTotal: req.Occurrences,
		})
		if err != nil {
			return "❌ Não consegui salvar a recorrência. Tente novamente.", err
		}
		entries[i] = entry
	}

	if err := h.store.SaveEntries(ctx, entries); err != nil {
		return "❌ Não consegui salvar a recorrência. Tente novamente.", err
	}

	return formatRecurrenceConfirmation(req, entries), nil
}

func (h *Handler) Resumo(ctx context.Context, userID string) (string, error) {
	now := time.Now().In(h.loc)
	yearMonth := domain.MonthOf(now)
	summary, err := h.store.MonthlySummary(ctx, userID, yearMonth)
	if err != nil {
		return "", fmt.Errorf("resumo: %w", err)
	}

	due := now.AddDate(0, 0, 1)
	tomorrow := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.UTC)
	pending, err := h.store.ListEntries(ctx, userID, pkgfinance.EntryFilter{
		Status: domain.PaymentStatusPending,
		To:     &tomorrow,
	})
	if err != nil {
		return "", fmt.Errorf("resumo pending: %w", err)
	}

	var totalDue int64
	for _, e := range pending {
		totalDue += e.Amount
	}

	// Faturamento and entradas de caixa are named separately because they are
	// different numbers and the difference matters: this screen used to show
	// one broad "Receitas" total next to a "Meta Faturamento" computed on a
	// narrower figure, with nothing to explain why they disagreed.
	msg := "📊 *Resumo Financeiro — " + now.Format("01/2006") + "*\n\n"
	msg += fmt.Sprintf("🛒 *Faturamento (vendas):* R$%s\n", money(summary.TotalRevenue))
	msg += fmt.Sprintf("💰 *Entradas de caixa:* R$%s\n", money(summary.TotalCashIn))
	msg += fmt.Sprintf("💸 *Despesas:* R$%s\n", money(summary.TotalExpense))
	msg += fmt.Sprintf("💵 *Saldo previsto:* R$%s\n", money(summary.ExpectedBalance))

	goal, err := h.store.GetGoal(ctx, userID, yearMonth)
	if err == nil && goal.RevenueTarget > 0 {
		faturamentoPct := float64(summary.TotalRevenue) / float64(goal.RevenueTarget) * 100
		msg += fmt.Sprintf("\n🎯 *Meta Faturamento:* R$%s / R$%s (*%.0f%%*)\n", money(summary.TotalRevenue), money(goal.RevenueTarget), faturamentoPct)
	}
	if err == nil && goal.ExpenseTarget > 0 {
		expPct := float64(summary.TotalExpense) / float64(goal.ExpenseTarget) * 100
		msg += fmt.Sprintf("🚫 *Teto Despesas:* R$%s (*%.0f%%*)\n", money(goal.ExpenseTarget), expPct)
	}

	if len(pending) > 0 {
		msg += fmt.Sprintf("\n⏳ *A vencer amanhã:* R$%s (%d conta(s))", money(totalDue), len(pending))
	}
	msg += "\n\nComandos:\n/despesa, /receita, /pagar, /receber, /goal, /meta"
	return msg, nil
}

func (h *Handler) Goal(ctx context.Context, userID string) (string, error) {
	now := time.Now().In(h.loc)
	yearMonth := domain.MonthOf(now)
	summary, err := h.store.MonthlySummary(ctx, userID, yearMonth)
	if err != nil {
		return "", fmt.Errorf("goal: %w", err)
	}

	goal, err := h.store.GetGoal(ctx, userID, yearMonth)
	if err != nil {
		return "Nenhuma meta definida para este mês.", nil
	}

	faturamento := summary.TotalRevenue
	faturamentoPct := float64(faturamento) / float64(goal.RevenueTarget) * 100
	expPct := float64(summary.TotalExpense) / float64(goal.ExpenseTarget) * 100

	msg := "🎯 *Metas — " + now.Format("01/2006") + "*\n\n"
	msg += fmt.Sprintf("📈 *Faturamento:* R$%s / R$%s (*%.0f%%*)\n", money(faturamento), money(goal.RevenueTarget), faturamentoPct)
	msg += progressBar(faturamentoPct)
	msg += fmt.Sprintf("\n📉 *Despesas:* R$%s / R$%s (*%.0f%%*)\n", money(summary.TotalExpense), money(goal.ExpenseTarget), expPct)
	msg += progressBar(expPct)
	msg += "\n\nDigite /resumo para ver o resumo completo."
	return msg, nil
}

// monthFaturamento is gone: it existed only because MonthlySummary could not
// tell a sale from any other inflow, so both /resumo and /meta paid for an
// extra ListEntries round-trip to work faturamento out for themselves. The
// summary now carries TotalRevenue and both read it directly.

func progressBar(pct float64) string {
	filled := int(pct / 10)
	if filled > 10 {
		filled = 10
	}
	if filled < 0 {
		filled = 0
	}
	bar := ""
	for i := 0; i < 10; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return bar + "\n"
}

func (h *Handler) SetGoal(ctx context.Context, userID, text string) (string, error) {
	parts := strings.Fields(text)
	if len(parts) < 3 {
		return commandTutorial("/meta"), nil
	}

	revenue, err := parseAmount(parts[1])
	if err != nil {
		return "Valor de faturamento inválido. Use números sem R$.\nEx: /meta 80000 60000", nil
	}
	exp, err := parseAmount(parts[2])
	if err != nil {
		return "Valor de despesa inválido. Use números sem R$.\nEx: /meta 80000 60000", nil
	}

	now := time.Now().In(h.loc)
	goal := domain.Goal{
		UserID:        userID,
		Month:         domain.MonthOf(now),
		RevenueTarget: revenue,
		ExpenseTarget: exp,
	}
	if err := h.store.SaveGoal(ctx, goal); err != nil {
		return "❌ Erro ao salvar meta.", err
	}

	msg := "✅ *Meta salva para " + now.Format("01/2006") + "*\n\n"
	msg += fmt.Sprintf("📈 *Faturamento:* R$%s\n", money(revenue))
	msg += fmt.Sprintf("📉 *Teto Despesas:* R$%s\n", money(exp))
	msg += "\nDigite /goal para ver o progresso."
	return msg, nil
}

func money(centavos int64) string {
	abs := centavos
	if abs < 0 {
		abs = -abs
	}
	s := fmt.Sprintf("%d,%02d", abs/100, abs%100)
	n := len(s)
	for i := n - 6; i > 0; i -= 4 {
		s = s[:i] + "." + s[i:]
	}
	if centavos < 0 {
		s = "-" + s
	}
	return s
}

func formatConfirmation(e domain.FinancialEntry) string {
	typeEmoji := "💸"
	typeLabel := "Despesa"
	if e.Type == domain.EntryTypeIncome {
		typeEmoji = "💰"
		typeLabel = "Receita"
	}

	statusLabel := "Pago ✅"
	if e.PaymentStatus == domain.PaymentStatusPending {
		statusLabel = "A pagar ⏳"
		if e.Type == domain.EntryTypeIncome {
			statusLabel = "A receber ⏳"
		}
	}

	msg := fmt.Sprintf("%s *%s registrada:*\n", typeEmoji, typeLabel)
	msg += fmt.Sprintf("💵 R$%s\n", e.AmountReais())
	msg += fmt.Sprintf("📂 %s\n", e.Category)
	if e.Description != "" {
		msg += fmt.Sprintf("📝 %s\n", e.Description)
	}
	msg += fmt.Sprintf("📅 %s\n", e.TransactionDate.Format("02/01/2006"))
	msg += fmt.Sprintf("Status: %s", statusLabel)

	if e.DueDate != nil {
		msg += fmt.Sprintf("\nVencimento: %s", e.DueDate.Format("02/01/2006"))
	}

	msg += "\n\nDigite /resumo para ver o saldo."
	return msg
}

func formatRecurrenceConfirmation(req recurrenceRequest, entries []domain.FinancialEntry) string {
	typeEmoji, typeLabel, statusLabel := "💸", "Despesa", "A pagar ⏳"
	if req.Type == domain.EntryTypeIncome {
		typeEmoji, typeLabel, statusLabel = "💰", "Receita", "A receber ⏳"
	}

	first, last := entries[0], entries[len(entries)-1]

	msg := fmt.Sprintf("%s *%s recorrente registrada:*\n", typeEmoji, typeLabel)
	msg += fmt.Sprintf("💵 R$%s x %d (%s)\n", first.AmountReais(), req.Occurrences, periodLabel(req.Period))
	msg += fmt.Sprintf("📂 %s\n", req.Category)
	if req.Description != "" {
		msg += fmt.Sprintf("📝 %s\n", req.Description)
	}
	msg += fmt.Sprintf("📅 %s até %s\n", first.DueDate.Format("02/01/2006"), last.DueDate.Format("02/01/2006"))
	msg += "Status: " + statusLabel
	msg += "\n\nDigite /resumo para ver o saldo."
	return msg
}

func parseAmount(s string) (int64, error) {
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", "")
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return v * 100, nil
}
