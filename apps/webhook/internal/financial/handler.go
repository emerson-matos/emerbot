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

// Handler processes financial commands from WhatsApp messages.
type Handler struct {
	parser whatsapp.Parser
	store  pkgfinance.Store
}

func NewHandler(parser whatsapp.Parser, store pkgfinance.Store) *Handler {
	return &Handler{parser: parser, store: store}
}

// commandTutorial returns a PT-BR, tutorial-style usage guide for a single
// command, or "" for an unknown one. It is the single source of the per-command
// "como usar" text — shown when a command is sent without (enough) arguments.
func commandTutorial(cmd string) string {
	switch strings.ToLower(cmd) {
	case "/despesa":
		return "*/despesa <valor> <categoria> [descrição]*\n" +
			"Registra uma despesa *já paga*.\n" +
			"Ex: /despesa 500 aluguel Aluguel da loja\n\n" +
			"💡 Para uma despesa *ainda não paga*, use */pagar* — ela fica pendente até você quitar.\n" +
			"Ex: /pagar 300 luz 20/07"
	case "/pagar":
		return "*/pagar <valor> <categoria> [data] [descrição]*\n" +
			"Agenda uma despesa *a pagar* (fica pendente). A data de vencimento (dd/mm) é opcional.\n" +
			"Ex: /pagar 300 luz 20/07 Conta de luz"
	case "/receita":
		return "*/receita <valor> <categoria> [descrição]*\n" +
			"Registra uma receita *já recebida*.\n" +
			"Ex: /receita 800 venda_balcao\n\n" +
			"💡 Para algo *a receber*, use */receber*."
	case "/receber":
		return "*/receber <valor> <categoria> [data] [descrição]*\n" +
			"Agenda uma receita *a receber* (fica pendente). A data (dd/mm) é opcional.\n" +
			"Ex: /receber 800 cliente_x 25/07"
	case "/meta":
		return "*/meta <faturamento> <despesa>*\n" +
			"Define as metas do mês (valores sem R$).\n" +
			"Ex: /meta 80000 60000"
	default:
		return ""
	}
}

// bareCommandUsage returns the command tutorial when text is only the command
// word (e.g. "/despesa" with no arguments), or "" when the text carries
// arguments so normal parsing proceeds.
func bareCommandUsage(text string) string {
	fields := strings.Fields(text)
	if len(fields) != 1 {
		return ""
	}
	return commandTutorial(fields[0])
}

// Handle parses a WhatsApp command, saves the entry, and returns
// a confirmation message in Portuguese for the bot to reply with.
func (h *Handler) Handle(ctx context.Context, userID, text string) (string, error) {
	// A bare command (just the verb, no arguments) is treated as a request for
	// help — teach the syntax instead of failing to parse it.
	if usage := bareCommandUsage(text); usage != "" {
		return usage, nil
	}

	parsed, err := h.parser.Parse(ctx, text)
	if err != nil {
		return fmt.Sprintf("❌ Não consegui entender. Tente:\n/despesa 500 aluguel\n/receita 800 venda_balcao\n/pagar 300 luz 20/07\n\nErro: %s", err.Error()), nil
	}

	status := domain.PaymentStatusPaid
	if parsed.IsPending {
		status = domain.PaymentStatusPending
	}

	now := time.Now().UTC()
	entry := domain.FinancialEntry{
		UserID:        userID,
		EntryID:       uuid.New().String(),
		Date:          now,
		Amount:        parsed.Amount,
		Category:      parsed.Category,
		Type:          parsed.Type,
		Description:   parsed.Description,
		DueDate:       parsed.DueDate,
		PaymentStatus: status,
		Source:        "whatsapp",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := h.store.SaveEntry(ctx, entry); err != nil {
		return "❌ Não consegui salvar. Tente novamente.", err
	}

	return formatConfirmation(entry), nil
}

func (h *Handler) Resumo(ctx context.Context, userID string) (string, error) {
	now := time.Now().UTC()
	yearMonth := now.Format("2006-01")
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

	msg := "📊 *Resumo Financeiro — " + now.Format("01/2006") + "*\n\n"
	msg += fmt.Sprintf("💰 *Receitas:* R$%s\n", money(summary.TotalIncome))
	msg += fmt.Sprintf("💸 *Despesas:* R$%s\n", money(summary.TotalExpense))
	msg += fmt.Sprintf("💵 *Saldo:* R$%s\n", money(summary.Balance))

	goal, err := h.store.GetGoal(ctx, userID, yearMonth)
	if err == nil {
		revPct := float64(summary.TotalIncome) / float64(goal.RevenueTarget) * 100
		expPct := float64(summary.TotalExpense) / float64(goal.ExpenseTarget) * 100
		msg += fmt.Sprintf("\n🎯 *Meta Faturamento:* R$%s (*%.0f%%*)\n", money(goal.RevenueTarget), revPct)
		msg += fmt.Sprintf("🚫 *Teto Despesas:* R$%s (*%.0f%%*)\n", money(goal.ExpenseTarget), expPct)
	}

	if len(pending) > 0 {
		msg += fmt.Sprintf("\n⏳ *A vencer amanhã:* R$%s (%d conta(s))", money(totalDue), len(pending))
	}
	msg += "\n\nComandos:\n/despesa, /receita, /pagar, /receber, /goal, /meta"
	return msg, nil
}

func (h *Handler) Goal(ctx context.Context, userID string) (string, error) {
	now := time.Now().UTC()
	yearMonth := now.Format("2006-01")
	summary, err := h.store.MonthlySummary(ctx, userID, yearMonth)
	if err != nil {
		return "", fmt.Errorf("goal: %w", err)
	}

	goal, err := h.store.GetGoal(ctx, userID, yearMonth)
	if err != nil {
		return "Nenhuma meta definida para este mês.", nil
	}

	revPct := float64(summary.TotalIncome) / float64(goal.RevenueTarget) * 100
	expPct := float64(summary.TotalExpense) / float64(goal.ExpenseTarget) * 100

	msg := "🎯 *Metas — " + now.Format("01/2006") + "*\n\n"
	msg += fmt.Sprintf("📈 *Faturamento:* R$%s / R$%s (*%.0f%%*)\n", money(summary.TotalIncome), money(goal.RevenueTarget), revPct)
	msg += progressBar(revPct)
	msg += fmt.Sprintf("\n📉 *Despesas:* R$%s / R$%s (*%.0f%%*)\n", money(summary.TotalExpense), money(goal.ExpenseTarget), expPct)
	msg += progressBar(expPct)
	msg += "\n\nDigite /resumo para ver o resumo completo."
	return msg, nil
}

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

	rev, err := parseAmount(parts[1])
	if err != nil {
		return "Valor de faturamento inválido. Use números sem R$.\nEx: /meta 80000 60000", nil
	}
	exp, err := parseAmount(parts[2])
	if err != nil {
		return "Valor de despesa inválido. Use números sem R$.\nEx: /meta 80000 60000", nil
	}

	now := time.Now().UTC()
	goal := domain.Goal{
		UserID:        userID,
		Month:         now.Format("2006-01"),
		RevenueTarget: rev,
		ExpenseTarget: exp,
	}
	if err := h.store.SaveGoal(ctx, goal); err != nil {
		return "❌ Erro ao salvar meta.", err
	}

	msg := "✅ *Meta salva para " + now.Format("01/2006") + "*\n\n"
	msg += fmt.Sprintf("📈 *Faturamento:* R$%s\n", money(rev))
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
	msg += fmt.Sprintf("📅 %s\n", e.Date.Format("02/01/2006"))
	msg += fmt.Sprintf("Status: %s", statusLabel)

	if e.DueDate != nil {
		msg += fmt.Sprintf("\nVencimento: %s", e.DueDate.Format("02/01/2006"))
	}

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
