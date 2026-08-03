package finance

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/domain"
)

// GoalStore is the slice of the finance store the goal endpoints use.
type GoalStore interface {
	GetGoal(ctx context.Context, userID, month string) (domain.Goal, error)
	SaveGoal(ctx context.Context, goal domain.Goal) error
}

type GoalsHandler struct {
	store GoalStore
	// loc is the calendar the pharmacy reasons about days in, for "current
	// month" defaults. See shared.PharmacyLocation.
	loc *time.Location
}

func NewGoalsHandler(store GoalStore, loc *time.Location) *GoalsHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &GoalsHandler{store: store, loc: loc}
}

// Get handles GET /goals?month=2026-07
func (h *GoalsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	month, err := httpx.Month(r, h.loc)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	goal, err := h.store.GetGoal(r.Context(), claims.UserID, month)
	if err != nil {
		httpx.OK(w, map[string]any{"goal": nil, "month": month})
		return
	}
	httpx.OK(w, map[string]any{"goal": goal, "month": month})
}

// Save handles PUT /goals
func (h *GoalsHandler) Save(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		Month         string `json:"month"`
		RevenueTarget *int64 `json:"revenue_target"`
		ExpenseTarget *int64 `json:"expense_target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	month := body.Month
	if month == "" {
		month = domain.CurrentMonth(h.loc)
	}
	// Validated up front, and for real: the previous check only looked at
	// months starting with "20" that contained exactly one dash, so "julho"
	// and "1999-7" were both stored verbatim as a goal's month.
	if _, _, err := domain.ParseMonth(month); err != nil {
		httpx.Error(w, "invalid month format, use YYYY-MM", http.StatusBadRequest)
		return
	}

	if body.RevenueTarget == nil && body.ExpenseTarget == nil {
		httpx.Error(w, "provide at least one of revenue_target or expense_target", http.StatusBadRequest)
		return
	}

	goal := domain.Goal{
		UserID: claims.UserID,
		Month:  month,
	}

	if body.RevenueTarget != nil {
		if *body.RevenueTarget < 0 {
			httpx.Error(w, "revenue_target must be >= 0", http.StatusBadRequest)
			return
		}
		goal.RevenueTarget = *body.RevenueTarget
	}

	if body.ExpenseTarget != nil {
		if *body.ExpenseTarget < 0 {
			httpx.Error(w, "expense_target must be >= 0", http.StatusBadRequest)
			return
		}
		goal.ExpenseTarget = *body.ExpenseTarget
	}

	if err := h.store.SaveGoal(r.Context(), goal); err != nil {
		log.Printf("save goal error: %v", err)
		httpx.Error(w, "failed to save goal", http.StatusInternalServerError)
		return
	}

	httpx.OK(w, map[string]any{"goal": goal})
}
