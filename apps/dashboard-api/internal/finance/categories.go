package finance

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/domain"
)

// CategoryStore is the slice of the finance store the category endpoints use.
type CategoryStore interface {
	ListCategories(ctx context.Context, userID string) ([]domain.Category, error)
	SaveCategory(ctx context.Context, cat domain.Category) error
}

type CategoriesHandler struct {
	store CategoryStore
}

func NewCategoriesHandler(store CategoryStore) *CategoriesHandler {
	return &CategoriesHandler{store: store}
}

// List handles GET /categories
func (h *CategoriesHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	cats, err := h.store.ListCategories(r.Context(), claims.UserID)
	if err != nil {
		httpx.Error(w, "failed to list categories", http.StatusInternalServerError)
		return
	}

	// Reconcile the user's categories against the current default definitions:
	// missing defaults are seeded and drifted ones (label/type/default) are
	// refreshed, so editing DefaultCategories reaches existing users on their
	// next read.
	changes := domain.ReconcileDefaultCategories(claims.UserID, cats)
	for _, c := range changes {
		h.store.SaveCategory(r.Context(), c) //nolint:errcheck
	}

	bySlug := make(map[string]domain.Category, len(cats)+len(changes))
	for _, c := range cats {
		bySlug[c.Slug] = c
	}
	for _, c := range changes {
		bySlug[c.Slug] = c
	}

	merged := make([]domain.Category, 0, len(bySlug))
	for _, c := range bySlug {
		merged = append(merged, c)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Slug < merged[j].Slug })

	httpx.OK(w, map[string]any{"categories": merged})
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
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Label) == "" {
		httpx.Error(w, "label is required", http.StatusBadRequest)
		return
	}
	entryType := domain.EntryType(req.Type)
	if entryType != domain.EntryTypeExpense && entryType != domain.EntryTypeIncome {
		httpx.Error(w, "type must be 'expense' or 'income'", http.StatusBadRequest)
		return
	}

	// Through the same constructor the WhatsApp agent uses, so a category created
	// here and one created there are the same thing. The slug used to be taken
	// from the request body verbatim: "Venda Atacado", "venda-atacado" and
	// "vendaAtacado" were three categories of one, and every breakdown downstream
	// would have had to know that. A slug in the body is now a suggestion — it
	// goes through the same normalization as the label.
	cat, err := domain.NewCategory(claims.UserID, req.Label, req.Slug, entryType)
	if err != nil {
		httpx.Error(w, "label must contain letters or numbers", http.StatusBadRequest)
		return
	}
	if err := h.store.SaveCategory(r.Context(), cat); err != nil {
		httpx.Error(w, "failed to save category", http.StatusInternalServerError)
		return
	}
	httpx.JSON(w, http.StatusCreated, cat)
}
