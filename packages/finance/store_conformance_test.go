package finance

import (
	"context"
	"fmt"
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

func TestStoresAgreeOnMultiMonthlySummary(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		seedLedger(t, s)
		ctx := context.Background()

		// An entry before the window, and another user's entry inside it:
		// neither may reach the totals.
		outside := entry(t, "april", "2026-04-01", 99999)
		theirs := entry(t, "other-user", "2026-07-05", 88888)
		theirs.UserID = "u2"
		for _, e := range []domain.FinancialEntry{outside, theirs} {
			if err := s.SaveEntry(ctx, e); err != nil {
				t.Fatalf("seed %s: %v", e.EntryID, err)
			}
		}

		got, err := s.MultiMonthlySummary(context.Background(), "u1", []string{"2026-05", "2026-06", "2026-07"})
		if err != nil {
			t.Fatalf("multi monthly summary: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d summaries, want one per requested month: %+v", len(got), got)
		}
		// A month with no entries is present with zero totals, not absent, so
		// callers can index the result without a presence check.
		if may, ok := got["2026-05"]; !ok || may.TotalIncome != 0 || may.TotalExpense != 0 {
			t.Fatalf("May = %+v (present %v), want a zeroed summary", may, ok)
		}
		if june := got["2026-06"]; june.TotalIncome != 50000 || june.TotalExpense != 20000 {
			t.Fatalf("June = %+v, want income 50000 and expense 20000", june)
		}
		if july := got["2026-07"]; july.TotalIncome != 10000 || july.TotalExpense != 35000 {
			t.Fatalf("July = %+v, want income 10000 and expense 35000", july)
		}
		// Each summary must agree with what MonthlySummary reports alone,
		// otherwise the analysis and the dashboard show different numbers for
		// the same month.
		for _, month := range []string{"2026-05", "2026-06", "2026-07"} {
			single, err := s.MonthlySummary(context.Background(), "u1", month)
			if err != nil {
				t.Fatalf("monthly summary %s: %v", month, err)
			}
			if got[month] != single {
				t.Fatalf("%s: multi = %+v, single = %+v — the two disagree", month, got[month], single)
			}
		}
	})
}

func TestStoresAgreeOnMultiMonthlySummaryEdgeCases(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		seedLedger(t, s)
		ctx := context.Background()

		// No months requested is not an error: it yields nothing to query.
		empty, err := s.MultiMonthlySummary(ctx, "u1", nil)
		if err != nil {
			t.Fatalf("no months: %v", err)
		}
		if len(empty) != 0 {
			t.Fatalf("got %d summaries for no months, want none", len(empty))
		}

		// A month it cannot parse is an error everywhere, like every other
		// month-taking method.
		if _, err := s.MultiMonthlySummary(ctx, "u1", []string{"2026-07", "julho"}); err == nil {
			t.Fatal("expected an error for an unparseable month")
		}

		// Duplicates collapse, and order does not matter.
		got, err := s.MultiMonthlySummary(ctx, "u1", []string{"2026-07", "2026-06", "2026-07"})
		if err != nil {
			t.Fatalf("duplicate months: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d summaries, want duplicates collapsed to 2: %+v", len(got), got)
		}
	})
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
		goal := domain.Goal{UserID: "u1", Month: "2026-07", IncomeTarget: 5000, ExpenseTarget: 3000}
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

func TestStoresAgreeOnInsightSnapshot(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
		snapshot := []byte(`{"month":"2026-07","health":{"status":"boa"}}`)

		// Get on missing returns error.
		_, err := s.GetInsightSnapshot(ctx, "u1", "2026-07-20")
		if err == nil {
			t.Fatal("get missing snapshot: want error, got nil")
		}

		// Save and get roundtrip.
		if err := s.SaveInsightSnapshot(ctx, "u1", "2026-07-20", snapshot, now); err != nil {
			t.Fatalf("save snapshot: %v", err)
		}
		got, err := s.GetInsightSnapshot(ctx, "u1", "2026-07-20")
		if err != nil {
			t.Fatalf("get snapshot: %v", err)
		}
		if string(got.Snapshot) != string(snapshot) {
			t.Errorf("snapshot = %s, want %s", got.Snapshot, snapshot)
		}
		if !got.ComputedAt.Equal(now) {
			t.Errorf("computedAt = %v, want %v", got.ComputedAt, now)
		}

		// Different date returns not found.
		_, err = s.GetInsightSnapshot(ctx, "u1", "2026-07-21")
		if err == nil {
			t.Fatal("get different date: want error, got nil")
		}

		// Overwrite same date.
		newSnap := []byte(`{"month":"2026-07","health":{"status":"atencao"}}`)
		newTime := now.Add(1 * time.Hour)
		if err := s.SaveInsightSnapshot(ctx, "u1", "2026-07-20", newSnap, newTime); err != nil {
			t.Fatalf("overwrite snapshot: %v", err)
		}
		got, err = s.GetInsightSnapshot(ctx, "u1", "2026-07-20")
		if err != nil {
			t.Fatalf("get overwritten: %v", err)
		}
		if string(got.Snapshot) != string(newSnap) {
			t.Errorf("snapshot after overwrite = %s, want %s", got.Snapshot, newSnap)
		}
	})
}

// The listing tools total a period by reading it whole; the two stores reach
// that period differently (a GSI2SK BETWEEN condition vs. an in-memory scan),
// so the month's total is exactly the kind of answer that could drift between
// them. It is the answer the bot puts in front of the user, so it is pinned
// against both.
func TestStoresAgreeOnListDueEntriesTotals(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		var want int64
		for d := 1; d <= 31; d++ {
			amount := int64(10_000 + d)
			want += amount
			// Registered in July, due in August: the bills the pharmacy is
			// asking about are pending ones, keyed by DueDate.
			e := entry(t, fmt.Sprintf("ago-%02d", d), "2026-07-20", amount,
				withDue(t, fmt.Sprintf("2026-08-%02d", d)))
			if err := s.SaveEntry(ctx, e); err != nil {
				t.Fatalf("seed day %d: %v", d, err)
			}
		}

		h := handlerFor(t, s, "list_due_entries")
		out := callTool(t, h, "u1", map[string]any{
			"from":  "2026-08-01",
			"to":    "2026-08-31",
			"limit": 5, // a period is its own bound; this must not cut it
		})
		env, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected listing envelope, got %T", out)
		}
		if got := len(entriesOf(t, out)); got != 31 {
			t.Fatalf("expected the whole month from both stores, got %d rows", got)
		}
		if env["truncated"] != false {
			t.Fatal("a period that fits must not report a cut")
		}
		if env["total_matching"] != 31 {
			t.Fatalf("total_matching = %v, want 31", env["total_matching"])
		}
		if env["total_expense"] != centavosToReais(want) {
			t.Fatalf("total_expense = %v, want %v", env["total_expense"], centavosToReais(want))
		}
	})
}
