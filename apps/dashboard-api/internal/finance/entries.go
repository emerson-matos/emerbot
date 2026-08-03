package finance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// defaultEntriesLimit/maxEntriesLimit bound how many entries a single
// GET /entries call can return, most-recent first — without this, an
// unbounded query (e.g. the web dashboard's Transações page browsing full
// history) would scan a user's entire DynamoDB partition on every request.
const (
	defaultEntriesLimit = 50
	maxEntriesLimit     = 200
)

// EntryStore is the slice of the finance store the entries endpoints use.
// Declaring it here, rather than depending on the 17-method pkgfinance.Store,
// keeps these handlers unaffected by methods added for other consumers — and
// lets a test double implement five methods instead of seventeen.
type EntryStore interface {
	ListEntries(ctx context.Context, userID string, filter pkgfinance.EntryFilter) ([]domain.FinancialEntry, error)
	GetEntry(ctx context.Context, userID string, date domain.CalendarDate, entryID string) (domain.FinancialEntry, error)
	SaveEntry(ctx context.Context, entry domain.FinancialEntry) error
	UpdateEntry(ctx context.Context, previous, updated domain.FinancialEntry) error
	DeleteEntry(ctx context.Context, userID string, date domain.CalendarDate, entryID string) error
}

// entryAddress reads the {date} and {id} path values that together address one
// entry.
//
// The date is in the URL because it is half the row's key (see
// pkgfinance.Store.GetEntry): without it the API can only find an entry by
// reading the user's whole ledger. It is also why an edit that moves the
// transaction date moves the entry's address — the response carries the new
// date, and the client is expected to follow it.
func entryAddress(r *http.Request) (domain.CalendarDate, string, error) {
	entryID := r.PathValue("id")
	if entryID == "" {
		return domain.CalendarDate{}, "", errors.New("entry id is required")
	}
	raw := r.PathValue("date")
	if raw == "" {
		return domain.CalendarDate{}, "", errors.New("entry date is required")
	}
	t, err := domain.ParseDay(raw)
	if err != nil {
		return domain.CalendarDate{}, "", errors.New("invalid entry date, expected YYYY-MM-DD")
	}
	return domain.NewCalendarDate(t), entryID, nil
}

// entryReadError maps a store failure to a status. Only a genuinely absent
// entry is a 404: answering that for a storage outage told the user their
// lançamento did not exist, which on a ledger is a different and much more
// alarming statement than "try again".
func entryReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, pkgfinance.ErrEntryNotFound) {
		httpx.Error(w, "entry not found", http.StatusNotFound)
		return
	}
	httpx.Error(w, "failed to read entry", http.StatusInternalServerError)
}

type EntriesHandler struct {
	store EntryStore
	// loc is the calendar the pharmacy reasons about days in. Marking an entry
	// paid records "today", and in UTC that is already tomorrow for part of
	// every evening in Brazil.
	loc *time.Location
}

// entryResponse is the transport shape; it intentionally has no JSON tags so
// encoding/json exposes Go's default field names.
type entryResponse struct {
	UserID                           string
	EntryID                          domain.EntryID
	TransactionDate                  string
	DueDate                          *string
	PaymentDate                      *string
	Amount                           int64
	Category                         string
	Description                      string
	Supplier                         string
	Type                             domain.EntryType
	PaymentStatus                    domain.PaymentStatus
	Source                           domain.EntrySource
	Origin                           domain.IncomeOrigin
	CreatedAt                        time.Time
	UpdatedAt                        time.Time
	RecurrenceID                     string
	RecurrenceIndex, RecurrenceTotal int
}

func responseEntry(e domain.FinancialEntry) entryResponse {
	r := entryResponse{UserID: e.UserID, EntryID: e.EntryID, TransactionDate: e.TransactionDate.String(), Amount: e.Amount, Category: e.Category, Description: e.Description, Supplier: e.Supplier, Type: e.Type, PaymentStatus: e.PaymentStatus, Source: e.Source, Origin: e.Origin, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, RecurrenceID: e.RecurrenceID, RecurrenceIndex: e.RecurrenceIndex, RecurrenceTotal: e.RecurrenceTotal}
	if e.DueDate != nil {
		s := e.DueDate.String()
		r.DueDate = &s
	}
	if e.PaymentDate != nil {
		s := e.PaymentDate.String()
		r.PaymentDate = &s
	}
	return r
}

// NewEntriesHandler builds the handler. loc is the timezone whose calendar day
// defines "today" when an entry is marked paid; nil falls back to UTC.
func NewEntriesHandler(store EntryStore, loc *time.Location) *EntriesHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &EntriesHandler{store: store, loc: loc}
}

// List handles GET /entries
func (h *EntriesHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	filter := pkgfinance.EntryFilter{}
	q := r.URL.Query()

	if from := q.Get("from"); from != "" {
		t, err := domain.ParseDay(from)
		if err != nil {
			httpx.Error(w, "invalid from date, expected YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		filter.From = &t
	}
	if to := q.Get("to"); to != "" {
		t, err := domain.ParseDay(to)
		if err != nil {
			httpx.Error(w, "invalid to date, expected YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		filter.To = &t
	}
	filter.Cursor = q.Get("cursor")
	filter.Category = q.Get("category")
	filter.Status = domain.PaymentStatus(q.Get("status"))
	filter.Type = domain.EntryType(q.Get("type"))
	filter.Origin = domain.NormalizeIncomeOrigin(q.Get("origin"))
	// Default limit only when scanning the full partition (no date range).
	// Date-bounded queries (used by the Dashboard's monthly view) return all
	// entries in the range so that summary calculations are not silently
	// truncated and the TransactionsTable shows every entry for the period.
	if q.Get("from") == "" && q.Get("to") == "" {
		filter.Limit = defaultEntriesLimit
	}
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if filter.Limit > maxEntriesLimit {
		filter.Limit = maxEntriesLimit
	}

	entries, err := h.store.ListEntries(r.Context(), claims.UserID, filter)
	if err != nil {
		httpx.Error(w, "failed to list entries", http.StatusInternalServerError)
		return
	}
	response := make([]entryResponse, len(entries))
	for i := range entries {
		response[i] = responseEntry(entries[i])
	}
	httpx.OK(w, map[string]any{"entries": response, "count": len(entries)})
}

type createEntryRequest struct {
	Date          string `json:"date"`   // "YYYY-MM-DD"
	Amount        int64  `json:"amount"` // centavos
	Category      string `json:"category"`
	Type          string `json:"type"` // "expense" | "income"
	Description   string `json:"description"`
	DueDate       string `json:"due_date"`       // "YYYY-MM-DD" or ""
	PaymentStatus string `json:"payment_status"` // "pending" | "paid"
	Supplier      string `json:"supplier"`
	Source        string `json:"source"`
	// Origin is where the money came from, for income entries. Empty defaults
	// to "venda" — the overwhelmingly common case for a pharmacy, and the
	// reading that keeps ordinary sales out of "Outros". An unknown value is
	// rejected rather than coerced: unlike the AI tools, this endpoint is
	// driven by a form with a fixed set of options, so a value outside the enum
	// is a client bug worth surfacing.
	Origin string `json:"origin"`
}

// incomeOrigin resolves the request's origin for the given entry type,
// reporting an error for a value outside the enum.
func (req createEntryRequest) incomeOrigin(entryType domain.EntryType) (domain.IncomeOrigin, error) {
	if entryType != domain.EntryTypeIncome {
		return "", nil
	}
	raw := strings.TrimSpace(req.Origin)
	if raw == "" {
		return domain.OriginVenda, nil
	}
	origin := domain.NormalizeIncomeOrigin(raw)
	if string(origin) != raw {
		return "", fmt.Errorf("invalid origin %q", raw)
	}
	return origin, nil
}

// Create handles POST /entries
func (h *EntriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	date := domain.NewCalendarDate(time.Now().UTC())
	if req.Date != "" {
		t, err := domain.ParseDay(req.Date)
		if err != nil {
			httpx.Error(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		date = domain.NewCalendarDate(t)
	}

	if req.Amount <= 0 {
		httpx.Error(w, "amount must be positive (in centavos)", http.StatusBadRequest)
		return
	}
	if req.Category == "" {
		httpx.Error(w, "category is required", http.StatusBadRequest)
		return
	}
	entryType := domain.EntryType(req.Type)
	if entryType != domain.EntryTypeExpense && entryType != domain.EntryTypeIncome {
		httpx.Error(w, "type must be 'expense' or 'income'", http.StatusBadRequest)
		return
	}

	status := domain.PaymentStatus(req.PaymentStatus)
	if status != domain.PaymentStatusPending && status != domain.PaymentStatusPaid {
		today := domain.NewCalendarDate(time.Now().UTC())
		if date.After(today) {
			status = domain.PaymentStatusPending
		} else {
			status = domain.PaymentStatusPaid
		}
	}

	// A malformed due date is rejected rather than dropped: silently saving the
	// entry without the date the user typed loses a bill's deadline, and every
	// due/overdue alert keys off that field.
	var dueDate *domain.CalendarDate
	if req.DueDate != "" {
		t, err := domain.ParseDay(req.DueDate)
		if err != nil {
			httpx.Error(w, "invalid due_date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		d := domain.NewCalendarDate(t)
		dueDate = &d
	}
	if status == domain.PaymentStatusPending && dueDate == nil {
		dueDate = &date
	}

	source := domain.EntrySource(strings.TrimSpace(req.Source))
	if source == "" {
		source = domain.SourceManual
	}
	origin, err := req.incomeOrigin(entryType)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entry, err := domain.NewFinancialEntry(domain.NewFinancialEntryInput{
		UserID:          claims.UserID,
		TransactionDate: date,
		Amount:          req.Amount,
		Category:        req.Category,
		Type:            entryType,
		Description:     req.Description,
		DueDate:         dueDate,
		PaymentStatus:   status,
		Supplier:        req.Supplier,
		Source:          source,
		Origin:          origin,
	})
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := entry.Validate(); err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.store.SaveEntry(r.Context(), entry); err != nil {
		httpx.Error(w, "failed to save entry", http.StatusInternalServerError)
		return
	}
	httpx.JSON(w, http.StatusCreated, responseEntry(entry))
}

// Get handles GET /entries/{date}/{id}
func (h *EntriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	date, entryID, err := entryAddress(r)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	entry, err := h.store.GetEntry(r.Context(), claims.UserID, date, entryID)
	if err != nil {
		entryReadError(w, err)
		return
	}
	httpx.OK(w, responseEntry(entry))
}

// updateEntryRequest is the PUT /entries/{id} body. Every field is a pointer
// because a patch has to tell "leave this alone" apart from "make this empty":
// the endpoint used to share createEntryRequest, where both read as the empty
// string, so a due date or a supplier typed by mistake could never be removed —
// and a mistyped transaction date could not be corrected at all, since the
// whole Date field was silently ignored.
type updateEntryRequest struct {
	Date          *string `json:"date"`   // "YYYY-MM-DD"
	Amount        *int64  `json:"amount"` // centavos
	Category      *string `json:"category"`
	Type          *string `json:"type"` // "expense" | "income"
	Description   *string `json:"description"`
	DueDate       *string `json:"due_date"`       // "YYYY-MM-DD", or "" to clear
	PaymentStatus *string `json:"payment_status"` // "pending" | "paid"
	Supplier      *string `json:"supplier"`
	Origin        *string `json:"origin"`
}

// apply writes the patch onto e, reporting the first field the client got
// wrong.
//
// Amount, Category, Type and PaymentStatus cannot be cleared — an entry always
// has all four, so an explicitly empty one is a client bug and answers 400
// rather than quietly changing nothing.
//
// It edits exactly the entry it is given. An occurrence of a /recorrente series
// is edited on its own like any other lançamento; applying a change across a
// whole series is deliberately not offered yet.
func (req updateEntryRequest) apply(e *domain.FinancialEntry, loc *time.Location) error {
	if req.Amount != nil {
		if *req.Amount <= 0 {
			return errors.New("amount must be positive (in centavos)")
		}
		e.Amount = *req.Amount
	}
	if req.Category != nil {
		if strings.TrimSpace(*req.Category) == "" {
			return errors.New("category is required")
		}
		e.Category = *req.Category
	}
	if req.Type != nil {
		t := domain.EntryType(*req.Type)
		if t != domain.EntryTypeExpense && t != domain.EntryTypeIncome {
			return errors.New("type must be 'expense' or 'income'")
		}
		e.Type = t
	}
	if req.Description != nil {
		e.Description = *req.Description
	}
	if req.Supplier != nil {
		e.Supplier = *req.Supplier
	}
	// Correcting the origin is the whole point of having the field: a loan
	// mislabelled as a sale has to be fixable, or it inflates faturamento
	// forever.
	//
	// An absent origin is left alone. Defaulting it to "venda" here would
	// silently relabel every pre-migration entry the first time anyone edited
	// its description — a loan filed under "Outros (Receita)", which
	// domain.IsRevenue's shim correctly keeps out of faturamento, would walk
	// into it. An entry with no origin stays that way until the backfill or a
	// deliberate edit gives it one. An explicitly empty origin *is* that
	// deliberate edit, and clears the field.
	if req.Origin != nil {
		raw := strings.TrimSpace(*req.Origin)
		origin := domain.NormalizeIncomeOrigin(raw)
		if string(origin) != raw {
			return fmt.Errorf("invalid origin %q", raw)
		}
		e.Origin = origin
	}
	if req.Date != nil {
		t, err := domain.ParseDay(*req.Date)
		if err != nil {
			return errors.New("invalid date format, use YYYY-MM-DD")
		}
		e.TransactionDate = domain.NewCalendarDate(t)
	}
	if req.DueDate != nil {
		if *req.DueDate == "" {
			e.DueDate = nil
		} else {
			t, err := domain.ParseDay(*req.DueDate)
			if err != nil {
				return errors.New("invalid due_date format, use YYYY-MM-DD")
			}
			d := domain.NewCalendarDate(t)
			e.DueDate = &d
		}
	}
	if req.PaymentStatus != nil {
		status := domain.PaymentStatus(*req.PaymentStatus)
		switch status {
		case domain.PaymentStatusPaid:
			if e.PaymentStatus != status || e.PaymentDate == nil {
				// Settling an entry happens now, not on the day the expense was
				// incurred. Using the transaction date made a bill registered on
				// the 14th and paid on the 26th report "pago em 14/07".
				d := domain.NewCalendarDate(time.Now().In(loc))
				e.PaymentDate = &d
			}
		case domain.PaymentStatusPending:
			e.PaymentDate = nil
		default:
			return errors.New("payment_status must be 'pending' or 'paid'")
		}
		e.PaymentStatus = status
	}
	return nil
}

// Update handles PUT /entries/{date}/{id}
func (h *EntriesHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	date, entryID, err := entryAddress(r)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	previous, err := h.store.GetEntry(r.Context(), claims.UserID, date, entryID)
	if err != nil {
		entryReadError(w, err)
		return
	}
	existing := previous

	var req updateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := req.apply(&existing, h.loc); err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing.UpdatedAt = time.Now().UTC()
	// Normalize before validating so switching an entry from income to expense
	// drops the origin it can no longer have, instead of failing on
	// "expense entry cannot have an income origin" with nothing the client can
	// send to fix it.
	existing.Normalize()

	// Validate before writing: the reverse order persisted the bad entry and
	// only then answered 400, leaving the caller with a rejection and the
	// ledger with the invalid row.
	if err := existing.Validate(); err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.UpdateEntry(r.Context(), previous, existing); err != nil {
		if errors.Is(err, pkgfinance.ErrEntryNotFound) {
			httpx.Error(w, "entry not found", http.StatusNotFound)
			return
		}
		httpx.Error(w, "failed to update entry", http.StatusInternalServerError)
		return
	}
	httpx.OK(w, responseEntry(existing))
}

// Delete handles DELETE /entries/{date}/{id}
func (h *EntriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	date, entryID, err := entryAddress(r)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.store.DeleteEntry(r.Context(), claims.UserID, date, entryID); err != nil {
		entryReadError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
