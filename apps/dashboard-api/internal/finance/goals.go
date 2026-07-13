package finance

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

type GoalsHandler struct {
	store pkgfinance.Store
}

func NewGoalsHandler(store pkgfinance.Store) *GoalsHandler {
	return &GoalsHandler{store: store}
}

// Get handles GET /goals?month=2026-07
func (h *GoalsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	goal, err := h.store.GetGoal(r.Context(), claims.UserID, month)
	if err != nil {
		jsonOK(w, map[string]any{"goal": nil, "month": month})
		return
	}
	jsonOK(w, map[string]any{"goal": goal, "month": month})
}

// Save handles PUT /goals
func (h *GoalsHandler) Save(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		Month         string `json:"month"`
		RevenueTarget *int64 `json:"revenue_target"`
		ExpenseTarget *int64 `json:"expense_target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	month := body.Month
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	if body.RevenueTarget == nil && body.ExpenseTarget == nil {
		jsonError(w, "provide at least one of revenue_target or expense_target", http.StatusBadRequest)
		return
	}

	goal := domain.Goal{
		UserID: claims.UserID,
		Month:  month,
	}

	if body.RevenueTarget != nil {
		if *body.RevenueTarget < 0 {
			jsonError(w, "revenue_target must be >= 0", http.StatusBadRequest)
			return
		}
		goal.RevenueTarget = *body.RevenueTarget
	}

	if body.ExpenseTarget != nil {
		if *body.ExpenseTarget < 0 {
			jsonError(w, "expense_target must be >= 0", http.StatusBadRequest)
			return
		}
		goal.ExpenseTarget = *body.ExpenseTarget
	}

	if strings.HasPrefix(month, "20") && strings.Count(month, "-") == 1 {
		parts := strings.Split(month, "-")
		if len(parts) == 2 && len(parts[0]) == 4 && len(parts[1]) == 2 {
			// valid month format
		} else {
			jsonError(w, "invalid month format, use YYYY-MM", http.StatusBadRequest)
			return
		}
	}

	if err := h.store.SaveGoal(r.Context(), goal); err != nil {
		log.Printf("save goal error: %v", err)
		jsonError(w, "failed to save goal", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]any{"goal": goal})
}
