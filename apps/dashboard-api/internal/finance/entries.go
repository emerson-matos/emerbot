package finance

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
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

type EntriesHandler struct {
	store pkgfinance.Store
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
	CreatedAt                        time.Time
	UpdatedAt                        time.Time
	RecurrenceID                     string
	RecurrenceIndex, RecurrenceTotal int
}

func responseEntry(e domain.FinancialEntry) entryResponse {
	r := entryResponse{UserID: e.UserID, EntryID: e.EntryID, TransactionDate: e.TransactionDate.String(), Amount: e.Amount, Category: e.Category, Description: e.Description, Supplier: e.Supplier, Type: e.Type, PaymentStatus: e.PaymentStatus, Source: e.Source, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, RecurrenceID: e.RecurrenceID, RecurrenceIndex: e.RecurrenceIndex, RecurrenceTotal: e.RecurrenceTotal}
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

func NewEntriesHandler(store pkgfinance.Store) *EntriesHandler {
	return &EntriesHandler{store: store}
}

// List handles GET /entries
func (h *EntriesHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	filter := pkgfinance.EntryFilter{}
	q := r.URL.Query()

	if from := q.Get("from"); from != "" {
		t, err := time.Parse("2006-01-02", from)
		if err == nil {
			filter.From = &t
		}
	}
	if to := q.Get("to"); to != "" {
		t, err := time.Parse("2006-01-02", to)
		if err == nil {
			filter.To = &t
		}
	}
	filter.Cursor = q.Get("cursor")
	filter.Category = q.Get("category")
	filter.Status = domain.PaymentStatus(q.Get("status"))
	filter.Type = domain.EntryType(q.Get("type"))
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
		jsonError(w, "failed to list entries", http.StatusInternalServerError)
		return
	}
	response := make([]entryResponse, len(entries))
	for i := range entries {
		response[i] = responseEntry(entries[i])
	}
	jsonOK(w, map[string]any{"entries": response, "count": len(entries)})
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
}

// Create handles POST /entries
func (h *EntriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	date := domain.NewCalendarDate(time.Now().UTC())
	if req.Date != "" {
		t, err := time.Parse("2006-01-02", req.Date)
		if err != nil {
			jsonError(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		date = domain.NewCalendarDate(t)
	}

	if req.Amount <= 0 {
		jsonError(w, "amount must be positive (in centavos)", http.StatusBadRequest)
		return
	}
	if req.Category == "" {
		jsonError(w, "category is required", http.StatusBadRequest)
		return
	}
	entryType := domain.EntryType(req.Type)
	if entryType != domain.EntryTypeExpense && entryType != domain.EntryTypeIncome {
		jsonError(w, "type must be 'expense' or 'income'", http.StatusBadRequest)
		return
	}

	status := domain.PaymentStatus(req.PaymentStatus)
	if status != domain.PaymentStatusPending && status != domain.PaymentStatusPaid {
		status = domain.PaymentStatusPaid
	}

	var dueDate *domain.CalendarDate
	if req.DueDate != "" {
		t, err := time.Parse("2006-01-02", req.DueDate)
		if err == nil {
			d := domain.NewCalendarDate(t)
			dueDate = &d
		}
	}

	source := domain.EntrySource(strings.TrimSpace(req.Source))
	if source == "" {
		source = domain.SourceManual
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
	})
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := entry.Validate(); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.store.SaveEntry(r.Context(), entry); err != nil {
		jsonError(w, "failed to save entry", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(responseEntry(entry)) //nolint:errcheck
}

// Update handles PUT /entries/{id}
func (h *EntriesHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	entryID := r.PathValue("id")
	if entryID == "" {
		jsonError(w, "entry id is required", http.StatusBadRequest)
		return
	}

	existing, err := h.store.GetEntry(r.Context(), claims.UserID, entryID)
	if err != nil {
		jsonError(w, "entry not found", http.StatusNotFound)
		return
	}

	var req createEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Apply updates, keep existing values for empty fields.
	if req.Amount > 0 {
		existing.Amount = req.Amount
	}
	if req.Category != "" {
		existing.Category = req.Category
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.Type != "" {
		existing.Type = domain.EntryType(req.Type)
	}
	if req.PaymentStatus != "" {
		existing.PaymentStatus = domain.PaymentStatus(req.PaymentStatus)
		if req.PaymentStatus == "paid" && existing.PaymentDate == nil {
			d := existing.TransactionDate
			existing.PaymentDate = &d
		}
		if req.PaymentStatus == "pending" {
			existing.PaymentDate = nil
		}
	}
	if req.Supplier != "" {
		existing.Supplier = req.Supplier
	}
	if req.DueDate != "" {
		t, err := time.Parse("2006-01-02", req.DueDate)
		if err == nil {
			d := domain.NewCalendarDate(t)
			existing.DueDate = &d
		}
	}
	existing.UpdatedAt = time.Now().UTC()

	if err := h.store.UpdateEntry(r.Context(), existing); err != nil {
		jsonError(w, "failed to update entry", http.StatusInternalServerError)
		return
	}
	if err := existing.Validate(); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, responseEntry(existing))
}

// Delete handles DELETE /entries/{id}
func (h *EntriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	entryID := r.PathValue("id")
	if entryID == "" {
		jsonError(w, "entry id is required", http.StatusBadRequest)
		return
	}

	if err := h.store.DeleteEntry(r.Context(), claims.UserID, entryID); err != nil {
		jsonError(w, "entry not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
