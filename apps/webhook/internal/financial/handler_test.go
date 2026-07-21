package financial

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/whatsapp"
)

func TestHandleReturnsFriendlyMessageOnParseError(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakeParser{err: errors.New("bad command")}, pkgfinance.NewInMemoryStore())

	msg, err := handler.Handle(context.Background(), "u1", "not a command")
	if err != nil {
		t.Fatalf("Handle returned unexpected error: %v", err)
	}
	if !strings.Contains(msg, "Não consegui entender") || !strings.Contains(msg, "bad command") {
		t.Fatalf("unexpected parse failure message: %s", msg)
	}
}

func TestHandleTeachesUsageForBareCommand(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	// A parser that would error proves the usage path short-circuits before parsing.
	handler := NewHandler(fakeParser{err: errors.New("should not be called")}, store)

	msg, err := handler.Handle(context.Background(), "u1", "/despesa")
	if err != nil {
		t.Fatalf("Handle returned unexpected error: %v", err)
	}
	// Teaches /despesa syntax and points to /pagar for unpaid expenses.
	if !strings.Contains(msg, "/despesa <valor>") || !strings.Contains(msg, "/pagar") {
		t.Fatalf("expected usage teaching /despesa and /pagar, got: %s", msg)
	}
	if strings.Contains(msg, "should not be called") {
		t.Fatalf("parser was invoked for a bare command: %s", msg)
	}

	// A bare command must not persist anything.
	entries, err := store.ListEntries(context.Background(), "u1", pkgfinance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("bare command saved %d entries, expected 0", len(entries))
	}
}

func TestHandleSavesParsedEntryWithPendingStatus(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{
		entry: whatsapp.ParsedEntry{
			Type:        domain.EntryTypeExpense,
			Amount:      12345,
			Category:    "energia_agua",
			Description: "Conta de luz",
			DueDate:     ptrTime(mustDate("2026-07-20")),
			IsPending:   true,
		},
	}, store)

	msg, err := handler.Handle(context.Background(), "u1", "/pagar 123,45 energia_agua 20/07")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !strings.Contains(msg, "Despesa registrada") || !strings.Contains(msg, "A pagar") {
		t.Fatalf("unexpected confirmation: %s", msg)
	}

	entries, err := store.ListEntries(context.Background(), "u1", pkgfinance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 saved entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.PaymentStatus != domain.PaymentStatusPending {
		t.Fatalf("expected pending status, got %s", entry.PaymentStatus)
	}
	if entry.Source != "whatsapp" {
		t.Fatalf("expected whatsapp source, got %s", entry.Source)
	}
	if entry.Category != "energia_agua" || entry.Amount != 12345 {
		t.Fatalf("unexpected saved entry: %+v", entry)
	}
}

func TestHandleUsesParsedDateForAlreadyOccurredEntry(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{
		entry: whatsapp.ParsedEntry{
			Type:        domain.EntryTypeExpense,
			Amount:      50000,
			Category:    "aluguel",
			Description: "Aluguel de julho",
			Date:        ptrTime(mustDate("2026-07-10")),
			IsPending:   false,
		},
	}, store)

	_, err := handler.Handle(context.Background(), "u1", "/despesa 500 aluguel 10/07 Aluguel de julho")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	entries, err := store.ListEntries(context.Background(), "u1", pkgfinance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 saved entry, got %d", len(entries))
	}
	if got := entries[0].Date; !got.Equal(mustDate("2026-07-10")) {
		t.Fatalf("expected entry.Date to use parsed date 2026-07-10, got %v", got)
	}
	if entries[0].DueDate != nil {
		t.Fatalf("expected no due date for non-pending entry, got %+v", entries[0].DueDate)
	}
}

func TestHandleSetsPaymentDateOnDespesa(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{
		entry: whatsapp.ParsedEntry{
			Type:        domain.EntryTypeExpense,
			Amount:      50000,
			Category:    "aluguel",
			Description: "Aluguel de julho",
			Date:        ptrTime(mustDate("2026-07-10")),
			IsPending:   false,
		},
	}, store)

	_, err := handler.Handle(context.Background(), "u1", "/despesa 500 aluguel 10/07 Aluguel de julho")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	entries, err := store.ListEntries(context.Background(), "u1", pkgfinance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 saved entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.PaymentDate == nil {
		t.Fatal("expected PaymentDate to be set for paid despesa entry")
	}
	if !(*entry.PaymentDate).Equal(entry.Date) {
		t.Fatalf("expected PaymentDate %v to equal Date %v", *entry.PaymentDate, entry.Date)
	}
}

func TestHandleDoesNotSetPaymentDateOnPagar(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{
		entry: whatsapp.ParsedEntry{
			Type:        domain.EntryTypeExpense,
			Amount:      12345,
			Category:    "energia_agua",
			Description: "Conta de luz",
			DueDate:     ptrTime(mustDate("2026-07-20")),
			IsPending:   true,
		},
	}, store)

	_, err := handler.Handle(context.Background(), "u1", "/pagar 123,45 energia_agua 20/07")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	entries, err := store.ListEntries(context.Background(), "u1", pkgfinance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 saved entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.PaymentDate != nil {
		t.Fatal("expected PaymentDate to be nil for pending pagar entry")
	}
}

func TestHandleDefaultsToNowWhenNoDateParsed(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{
		entry: whatsapp.ParsedEntry{
			Type:     domain.EntryTypeExpense,
			Amount:   50000,
			Category: "aluguel",
		},
	}, store)

	before := time.Now().UTC()
	_, err := handler.Handle(context.Background(), "u1", "/despesa 500 aluguel")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	after := time.Now().UTC()

	entries, err := store.ListEntries(context.Background(), "u1", pkgfinance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 saved entry, got %d", len(entries))
	}
	got := entries[0].Date
	if got.Before(before) || got.After(after) {
		t.Fatalf("expected entry.Date to default to now (between %v and %v), got %v", before, after, got)
	}
}

func TestRecorrenteSavesWholeSeriesWithSharedRecurrenceID(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{}, store)

	msg, err := handler.Recorrente(context.Background(), "u1", "/recorrente pagar 350 aluguel mensal 12 Aluguel anual")
	if err != nil {
		t.Fatalf("Recorrente returned error: %v", err)
	}
	if !strings.Contains(msg, "Despesa recorrente registrada") || !strings.Contains(msg, "x 12") {
		t.Fatalf("unexpected confirmation: %s", msg)
	}

	entries, err := store.ListEntries(context.Background(), "u1", pkgfinance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 12 {
		t.Fatalf("expected 12 saved entries, got %d", len(entries))
	}

	byIndex := make(map[int]domain.FinancialEntry, len(entries))
	recurrenceID := entries[0].RecurrenceID
	if recurrenceID == "" {
		t.Fatal("expected a non-empty RecurrenceID")
	}
	for _, e := range entries {
		if e.RecurrenceID != recurrenceID {
			t.Fatalf("expected all entries to share RecurrenceID %q, got %q", recurrenceID, e.RecurrenceID)
		}
		if e.RecurrenceTotal != 12 {
			t.Fatalf("expected RecurrenceTotal 12, got %d", e.RecurrenceTotal)
		}
		if e.PaymentStatus != domain.PaymentStatusPending {
			t.Fatalf("expected pending status, got %s", e.PaymentStatus)
		}
		if e.DueDate == nil {
			t.Fatal("expected a due date on every occurrence")
		}
		byIndex[e.RecurrenceIndex] = e
	}
	if len(byIndex) != 12 {
		t.Fatalf("expected 12 distinct RecurrenceIndex values, got %d", len(byIndex))
	}

	first, twelfth := byIndex[1], byIndex[12]
	monthsApart := (twelfth.DueDate.Year()-first.DueDate.Year())*12 + int(twelfth.DueDate.Month()-first.DueDate.Month())
	if monthsApart != 11 {
		t.Fatalf("expected occurrence 12 to be 11 months after occurrence 1, got %d months", monthsApart)
	}
}

func TestRecorrenteReturnsFriendlyMessageOnInvalidInput(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{}, store)

	msg, err := handler.Recorrente(context.Background(), "u1", "/recorrente pagar aluguel mensal 12")
	if err != nil {
		t.Fatalf("Recorrente returned unexpected error: %v", err)
	}
	if !strings.Contains(msg, "Não consegui entender") {
		t.Fatalf("expected friendly parse-error message, got: %s", msg)
	}

	entries, err := store.ListEntries(context.Background(), "u1", pkgfinance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid /recorrente saved %d entries, expected 0", len(entries))
	}
}

func TestSetGoalTeachesUsageWhenArgsMissing(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakeParser{}, pkgfinance.NewInMemoryStore())

	for _, text := range []string{"/meta", "/meta 80000"} {
		msg, err := handler.SetGoal(context.Background(), "u1", text)
		if err != nil {
			t.Fatalf("SetGoal(%q) error: %v", text, err)
		}
		if !strings.Contains(msg, "/meta <faturamento>") {
			t.Fatalf("SetGoal(%q) expected tutorial, got: %s", text, msg)
		}
	}
}

func TestSetGoalPersistsTargetsAndGoalReadsProgress(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{}, store)
	ctx := context.Background()

	for _, entry := range []domain.FinancialEntry{
		testFinancialEntry("u1", "income", time.Now().UTC(), 50000, "venda_balcao", domain.EntryTypeIncome),
		testFinancialEntry("u1", "expense", time.Now().UTC(), 20000, "aluguel", domain.EntryTypeExpense),
	} {
		if err := store.SaveEntry(ctx, entry); err != nil {
			t.Fatalf("SaveEntry(%s): %v", entry.EntryID, err)
		}
	}

	msg, err := handler.SetGoal(ctx, "u1", "/meta 80000 60000")
	if err != nil {
		t.Fatalf("SetGoal returned error: %v", err)
	}
	if !strings.Contains(msg, "Meta salva") || !strings.Contains(msg, "80.000,00") {
		t.Fatalf("unexpected set goal message: %s", msg)
	}

	goalMsg, err := handler.Goal(ctx, "u1")
	if err != nil {
		t.Fatalf("Goal returned error: %v", err)
	}
	if !strings.Contains(goalMsg, "Faturamento") || !strings.Contains(goalMsg, "1%") || !strings.Contains(goalMsg, "R$80.000,00") {
		t.Fatalf("unexpected goal message: %s", goalMsg)
	}
}

func TestGoalWithoutSavedTargetReturnsFriendlyMessage(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fakeParser{}, pkgfinance.NewInMemoryStore())

	msg, err := handler.Goal(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Goal returned unexpected error: %v", err)
	}
	if msg != "Nenhuma meta definida para este mês." {
		t.Fatalf("unexpected goal message: %s", msg)
	}
}

func TestResumoIncludesPendingTotals(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	handler := NewHandler(fakeParser{}, store)
	ctx := context.Background()

	now := time.Now().UTC()
	tomorrow := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)

	income := testFinancialEntry("u1", "income", now, 90000, "venda_balcao", domain.EntryTypeIncome)
	expense := testFinancialEntry("u1", "expense", now, 25000, "aluguel", domain.EntryTypeExpense)
	pending := testFinancialEntry("u1", "pending", now, 7000, "energia_agua", domain.EntryTypeExpense)
	pending.PaymentStatus = domain.PaymentStatusPending
	pending.DueDate = &tomorrow

	for _, entry := range []domain.FinancialEntry{income, expense, pending} {
		if err := store.SaveEntry(ctx, entry); err != nil {
			t.Fatalf("SaveEntry(%s): %v", entry.EntryID, err)
		}
	}

	msg, err := handler.Resumo(ctx, "u1")
	if err != nil {
		t.Fatalf("Resumo returned error: %v", err)
	}
	if !strings.Contains(msg, "Receitas:* R$900,00") {
		t.Fatalf("expected income in summary, got %s", msg)
	}
	if !strings.Contains(msg, "A vencer amanhã:* R$70,00 (1 conta(s))") {
		t.Fatalf("expected pending due summary, got %s", msg)
	}
}

type fakeParser struct {
	entry whatsapp.ParsedEntry
	err   error
}

func (f fakeParser) Parse(context.Context, string) (whatsapp.ParsedEntry, error) {
	if f.err != nil {
		return whatsapp.ParsedEntry{}, f.err
	}
	return f.entry, nil
}

func testFinancialEntry(userID, entryID string, date time.Time, amount int64, category string, entryType domain.EntryType) domain.FinancialEntry {
	return domain.FinancialEntry{
		UserID:        userID,
		EntryID:       entryID,
		Date:          date,
		Amount:        amount,
		Category:      category,
		Type:          entryType,
		Description:   category,
		PaymentStatus: domain.PaymentStatusPaid,
		CreatedAt:     date,
		UpdatedAt:     date,
	}
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
