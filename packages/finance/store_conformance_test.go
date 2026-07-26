package finance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore/dynamotest"
)

// Store has two implementations and callers pick one by configuration, so any
// question they answer differently is a bug that only shows up in one
// environment. This suite runs the same scenario against both.
//
// It exists because they had already drifted: MonthlySummary("julho") returned
// an error from DynamoDB and a zero-valued summary from the in-memory store,
// so a typo'd month rendered R$ 0,00 as real data locally and a 500 in
// production.

// eachStore runs fn against every Store implementation.
func eachStore(t *testing.T, fn func(t *testing.T, s Store)) {
	t.Helper()
	impls := map[string]func() Store{
		"inmemory": func() Store { return NewInMemoryStore() },
		"dynamodb": func() Store {
			tbl := dynamotest.New(dynamotest.Config{
				Name: testTable,
				Key:  dynamotest.Key{Hash: "PK", Range: "SK"},
				GSIs: map[string]dynamotest.Key{
					gsi2IndexName: {Hash: "GSI2PK", Range: "GSI2SK"},
				},
			})
			return NewDynamoDBStoreWithClient(tbl, testTable)
		},
	}
	for name, build := range impls {
		t.Run(name, func(t *testing.T) { fn(t, build()) })
	}
}

func seedLedger(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	for _, e := range []domain.FinancialEntry{
		entry(t, "jun-in", "2026-06-15", 50000, withIncome()),
		entry(t, "jun-out", "2026-06-20", 20000),
		entry(t, "jul-in", "2026-07-03", 10000, withIncome()),
		entry(t, "jul-out", "2026-07-02", 5000),
		entry(t, "jul-aluguel", "2026-07-05", 30000, withCategory("aluguel")),
	} {
		if err := s.SaveEntry(ctx, e); err != nil {
			t.Fatalf("seed %s: %v", e.EntryID, err)
		}
	}
}

func TestStoresAgreeOnMonthlySummary(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		seedLedger(t, s)

		got, err := s.MonthlySummary(context.Background(), "u1", "2026-07")
		if err != nil {
			t.Fatalf("monthly summary: %v", err)
		}
		want := MonthlySummary{Month: "2026-07", TotalIncome: 10000, TotalExpense: 35000, Balance: -25000}
		if got != want {
			t.Fatalf("summary = %+v, want %+v", got, want)
		}
	})
}

func TestStoresAgreeOnAnUnparseableMonth(t *testing.T) {
	// The divergence that motivated this suite. A month the user typed wrong
	// must be an error everywhere — returning a zero summary presents "no data"
	// and "bad request" as the same thing on a financial dashboard.
	for _, month := range []string{"julho", "2026", "2026-13-01", ""} {
		t.Run(month, func(t *testing.T) {
			eachStore(t, func(t *testing.T, s Store) {
				seedLedger(t, s)
				ctx := context.Background()

				if _, err := s.MonthlySummary(ctx, "u1", month); err == nil {
					t.Fatalf("MonthlySummary(%q) returned no error", month)
				}
				if _, err := s.CashFlowForecast(ctx, "u1", month); err == nil {
					t.Fatalf("CashFlowForecast(%q) returned no error", month)
				}
			})
		})
	}
}

func TestStoresAgreeOnDueDateBucketing(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		// A /recorrente-style pending expense registered in July but due in
		// September counts toward September, not July. An already-settled July
		// expense (no DueDate) still counts toward July.
		for _, e := range []domain.FinancialEntry{
			entry(t, "installment", "2026-07-14", 35000, withCategory("aluguel"), withDue(t, "2026-09-10")),
			entry(t, "settled", "2026-07-05", 5000, withCategory("aluguel")),
		} {
			if err := s.SaveEntry(ctx, e); err != nil {
				t.Fatalf("seed %s: %v", e.EntryID, err)
			}
		}

		july, err := s.MonthlySummary(ctx, "u1", "2026-07")
		if err != nil {
			t.Fatalf("July summary: %v", err)
		}
		if july.TotalExpense != 5000 {
			t.Fatalf("July expense = %d, want 5000 — the September-due installment leaked in", july.TotalExpense)
		}

		september, err := s.MonthlySummary(ctx, "u1", "2026-09")
		if err != nil {
			t.Fatalf("September summary: %v", err)
		}
		if september.TotalExpense != 35000 {
			t.Fatalf("September expense = %d, want the pending installment counted at 35000", september.TotalExpense)
		}
	})
}

func TestStoresAgreeOnCategorySummary(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		seedLedger(t, s)

		got, err := s.CategorySummary(context.Background(), "u1", *day(t, "2026-07-01"), *day(t, "2026-07-31"))
		if err != nil {
			t.Fatalf("category summary: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d categories, want 2: %+v", len(got), got)
		}
		// Largest total first.
		if got[0].Category != "aluguel" || got[0].Total != 30000 {
			t.Fatalf("first category = %+v, want aluguel/30000", got[0])
		}
		if got[1].Category != "mercado" || got[1].Total != 15000 || got[1].Count != 2 {
			t.Fatalf("second category = %+v, want mercado/15000/2", got[1])
		}
	})
}

func TestStoresAgreeOnCashFlowForecast(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		seedLedger(t, s)

		points, err := s.CashFlowForecast(context.Background(), "u1", "2026-07")
		if err != nil {
			t.Fatalf("cash flow forecast: %v", err)
		}
		if len(points) != 31 {
			t.Fatalf("got %d points, want 31", len(points))
		}
		// June's 50000 in and 20000 out carry in as the opening balance.
		if points[0].RunningBalance != 30000 {
			t.Fatalf("day 1 balance = %d, want 30000 carried in from June", points[0].RunningBalance)
		}
		if points[1].ProjectedExpense != 5000 || points[1].RunningBalance != 25000 {
			t.Fatalf("day 2 = %+v, want expense 5000 and balance 25000", points[1])
		}
		if points[2].ProjectedIncome != 10000 || points[2].RunningBalance != 35000 {
			t.Fatalf("day 3 = %+v, want income 10000 and balance 35000", points[2])
		}
		if last := points[30]; last.Date != "2026-07-31" || last.RunningBalance != 5000 {
			t.Fatalf("last point = %+v, want 2026-07-31 at 5000", last)
		}
	})
}

func TestStoresAgreeOnEntryLifecycle(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		e := entry(t, "e1", "2026-07-10", 1000)

		if err := s.SaveEntry(ctx, e); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := s.GetEntry(ctx, "u1", "e1")
		if err != nil || got.Amount != 1000 {
			t.Fatalf("get = %+v (err %v), want the saved entry", got, err)
		}

		e.Amount = 2000
		if err := s.UpdateEntry(ctx, e); err != nil {
			t.Fatalf("update: %v", err)
		}
		if got, _ = s.GetEntry(ctx, "u1", "e1"); got.Amount != 2000 {
			t.Fatalf("amount after update = %d, want 2000", got.Amount)
		}

		if err := s.DeleteEntry(ctx, "u1", "e1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := s.GetEntry(ctx, "u1", "e1"); err == nil {
			t.Fatal("expected an error getting a deleted entry")
		}
	})
}

func TestStoresAgreeOnMissingEntryErrors(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for name, call := range map[string]func() error{
			"get":    func() error { _, err := s.GetEntry(ctx, "u1", "ghost"); return err },
			"update": func() error { return s.UpdateEntry(ctx, entry(t, "ghost", "2026-07-10", 100)) },
			"delete": func() error { return s.DeleteEntry(ctx, "u1", "ghost") },
		} {
			if err := call(); err == nil {
				t.Fatalf("%s on a missing entry returned no error", name)
			}
		}
	})
}

func TestStoresAgreeOnEntryFilters(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, e := range []domain.FinancialEntry{
			entry(t, "e1", "2026-07-10", 100, withCategory("mercado"), withDescription("Pão na Padaria")),
			entry(t, "e2", "2026-07-11", 200, withCategory("aluguel"), withPaid(t, "2026-07-11")),
			entry(t, "e3", "2026-07-12", 300, withCategory("mercado"), withIncome()),
		} {
			if err := s.SaveEntry(ctx, e); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}

		cases := map[string]struct {
			filter EntryFilter
			want   []string
		}{
			"category":        {EntryFilter{Category: "mercado"}, []string{"e3", "e1"}},
			"status":          {EntryFilter{Status: domain.PaymentStatusPaid}, []string{"e2"}},
			"type":            {EntryFilter{Type: domain.EntryTypeIncome}, []string{"e3"}},
			"description":     {EntryFilter{Description: "PADARIA"}, []string{"e1"}},
			"limit":           {EntryFilter{Limit: 2}, []string{"e3", "e2"}},
			"range":           {EntryFilter{From: day(t, "2026-07-11"), To: day(t, "2026-07-12")}, []string{"e3", "e2"}},
			"nothing matches": {EntryFilter{Category: "inexistente"}, nil},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				got, err := s.ListEntries(ctx, "u1", tc.filter)
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if strings.Join(ids(got), ",") != strings.Join(tc.want, ",") {
					t.Fatalf("ids = %v, want %v", ids(got), tc.want)
				}
			})
		}
	})
}

func TestStoresAgreeOnGoalsAndCategories(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()

		if _, err := s.GetGoal(ctx, "u1", "2026-07"); err == nil {
			t.Fatal("expected an error for a month with no goal")
		}
		goal := domain.Goal{UserID: "u1", Month: "2026-07", RevenueTarget: 5000, ExpenseTarget: 3000}
		if err := s.SaveGoal(ctx, goal); err != nil {
			t.Fatalf("save goal: %v", err)
		}
		if got, err := s.GetGoal(ctx, "u1", "2026-07"); err != nil || got != goal {
			t.Fatalf("goal = %+v (err %v), want %+v", got, err, goal)
		}

		for _, c := range []domain.Category{
			{UserID: "u1", Slug: "mercado", Label: "Mercado", Type: domain.EntryTypeExpense},
			{UserID: "u1", Slug: "aluguel", Label: "Aluguel", Type: domain.EntryTypeExpense},
		} {
			if err := s.SaveCategory(ctx, c); err != nil {
				t.Fatalf("save category: %v", err)
			}
		}
		cats, err := s.ListCategories(ctx, "u1")
		if err != nil {
			t.Fatalf("list categories: %v", err)
		}
		// Sorted by slug in both implementations.
		if len(cats) != 2 || cats[0].Slug != "aluguel" || cats[1].Slug != "mercado" {
			t.Fatalf("categories = %+v, want aluguel then mercado", cats)
		}
	})
}

func TestStoresAgreeOnNotifications(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()

		if _, err := s.GetNotificationPrefs(ctx, "u1"); err == nil {
			t.Fatal("expected an error for a user with no prefs")
		}
		prefs := domain.NotificationPrefs{UserID: "u1", WAEnabled: true, Phone: "5511999", NotifyGoal: true}
		if err := s.SaveNotificationPrefs(ctx, prefs); err != nil {
			t.Fatalf("save prefs: %v", err)
		}
		if got, err := s.GetNotificationPrefs(ctx, "u1"); err != nil || got != prefs {
			t.Fatalf("prefs = %+v (err %v), want %+v", got, err, prefs)
		}

		all, err := s.ListNotificationPrefs(ctx)
		if err != nil || len(all) != 1 {
			t.Fatalf("list prefs = %+v (err %v), want one entry", all, err)
		}

		sent, err := s.NotificationSent(ctx, "u1", "2026-07-20")
		if err != nil || sent {
			t.Fatalf("sent = %v (err %v), want false", sent, err)
		}
		if err := s.RecordNotificationSent(ctx, "u1", "2026-07-20", time.Now()); err != nil {
			t.Fatalf("record: %v", err)
		}
		if sent, err = s.NotificationSent(ctx, "u1", "2026-07-20"); err != nil || !sent {
			t.Fatalf("sent = %v (err %v), want true", sent, err)
		}
	})
}
