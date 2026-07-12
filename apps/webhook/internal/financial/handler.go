package financial

import (
	"context"
	"fmt"
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

// Handle parses a WhatsApp command, saves the entry, and returns
// a confirmation message in Portuguese for the bot to reply with.
func (h *Handler) Handle(ctx context.Context, userID, text string) (string, error) {
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
