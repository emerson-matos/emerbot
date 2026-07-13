package finance

import (
	"encoding/json"
	"net/http"
	"strings"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

type CategoriesHandler struct {
	store pkgfinance.Store
}

func NewCategoriesHandler(store pkgfinance.Store) *CategoriesHandler {
	return &CategoriesHandler{store: store}
}

// List handles GET /categories
func (h *CategoriesHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	cats, err := h.store.ListCategories(r.Context(), claims.UserID)
	if err != nil {
		jsonError(w, "failed to list categories", http.StatusInternalServerError)
		return
	}

	// Seed defaults on first call if empty.
	if len(cats) == 0 {
		defaults := domain.DefaultCategories(claims.UserID)
		for _, c := range defaults {
			h.store.SaveCategory(r.Context(), c) //nolint:errcheck
		}
		cats = defaults
	}

	jsonOK(w, map[string]any{"categories": cats})
}

type createCategoryRequest struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
	Type  string `json:"type"` // "expense" | "income"
}

// Create handles POST /categories
func (h *CategoriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.Slug = strings.TrimSpace(req.Slug)
	req.Label = strings.TrimSpace(req.Label)
	if req.Slug == "" || req.Label == "" {
		jsonError(w, "slug and label are required", http.StatusBadRequest)
		return
	}

	entryType := domain.EntryType(req.Type)
	if entryType != domain.EntryTypeExpense && entryType != domain.EntryTypeIncome {
		jsonError(w, "type must be 'expense' or 'income'", http.StatusBadRequest)
		return
	}

	cat := domain.Category{
		UserID:  claims.UserID,
		Slug:    req.Slug,
		Label:   req.Label,
		Type:    entryType,
		Default: false,
	}
	if err := h.store.SaveCategory(r.Context(), cat); err != nil {
		jsonError(w, "failed to save category", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(cat) //nolint:errcheck
}
