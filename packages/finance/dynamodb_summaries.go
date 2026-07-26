package finance

import (
	"context"
	"time"
)

// The summaries are derived views over ListEntries, shared with InMemoryStore
// — see summaries.go.

func (s *DynamoDBStore) MonthlySummary(ctx context.Context, userID, yearMonth string) (MonthlySummary, error) {
	return monthlySummary(ctx, s, userID, yearMonth)
}

func (s *DynamoDBStore) MultiMonthlySummary(ctx context.Context, userID string, yearMonths []string) (map[string]MonthlySummary, error) {
	return multiMonthlySummary(ctx, s, userID, yearMonths)
}

func (s *DynamoDBStore) CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]CategorySummary, error) {
	return categorySummary(ctx, s, userID, from, to)
}

func (s *DynamoDBStore) CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]CashFlowPoint, error) {
	return cashFlowForecast(ctx, s, userID, yearMonth)
}
