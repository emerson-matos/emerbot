package payments

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore/dynamotest"
)

// DynamoDBRepository.Save is the interesting one: it replaces exactly one
// (Provider, SourceDate) import by diffing the new key set against the index
// item the previous run wrote. These tests drive that logic against an
// in-memory table so the replace, the idempotency and the recovery behaviour
// are all exercised for real.

const (
	testTable  = "emerbot-payments-test"
	testLedger = "shared-ledger"
)

func newRepo(t *testing.T, pageSize int) (*DynamoDBRepository, *dynamotest.Table) {
	t.Helper()
	tbl := dynamotest.New(dynamotest.Config{
		Name:     testTable,
		Key:      dynamotest.Key{Hash: "PK", Range: "SK"},
		PageSize: pageSize,
	})
	repo := NewDynamoDBRepositoryWithClient(tbl, testTable, testLedger)
	repo.backoffBase = time.Microsecond // walk the retry ladder without sleeping through it
	return repo, tbl
}

func cd(t *testing.T, s string) domain.CalendarDate {
	t.Helper()
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

func sale(t *testing.T, extID, date string, gross, net int64) Sale {
	t.Helper()
	return Sale{
		ID: NewSaleID(ProviderPagBank, extID), Provider: ProviderPagBank, ExternalID: extID,
		SaleDate: cd(t, date), GrossAmount: gross, NetAmount: net, FeeAmount: gross - net,
		Method: MethodCredito, Brand: "visa", Installments: 1,
	}
}

func receivable(t *testing.T, extID, expected string, amount int64, parcela int) ExpectedReceivable {
	t.Helper()
	return ExpectedReceivable{
		Provider: ProviderPagBank, SaleID: NewSaleID(ProviderPagBank, extID),
		ExpectedDate: cd(t, expected), Amount: amount,
		InstallmentNumber: parcela, InstallmentTotal: parcela,
	}
}

func payment(t *testing.T, extID, paid string, amount int64, parcela int) Payment {
	t.Helper()
	return Payment{
		Provider: ProviderPagBank, SaleID: NewSaleID(ProviderPagBank, extID),
		PaymentDate: cd(t, paid), Amount: amount, InstallmentNumber: parcela,
	}
}

func saveResult(t *testing.T, r *DynamoDBRepository, result ImportResult) {
	t.Helper()
	if err := r.Save(context.Background(), result); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func listSaleIDs(t *testing.T, r *DynamoDBRepository, from, to string) []string {
	t.Helper()
	sales, err := r.ListSales(context.Background(), cd(t, from), cd(t, to))
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	out := make([]string, 0, len(sales))
	for _, s := range sales {
		out = append(out, s.ExternalID)
	}
	return out
}

func TestSaveAndListRoundTrip(t *testing.T) {
	r, _ := newRepo(t, 0)
	original := sale(t, "A1", "2026-07-10", 10000, 9700)
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales:       []Sale{original},
		Receivables: []ExpectedReceivable{receivable(t, "A1", "2026-08-09", 9700, 1)},
		Payments:    []Payment{payment(t, "A1", "2026-07-11", 9700, 1)},
	})

	sales, err := r.ListSales(context.Background(), cd(t, "2026-07-01"), cd(t, "2026-07-31"))
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	if len(sales) != 1 {
		t.Fatalf("got %d sales, want 1", len(sales))
	}
	if sales[0] != original {
		t.Fatalf("sale = %+v, want %+v", sales[0], original)
	}

	// Receivables are keyed by their expected date, which is in a later month
	// than the sale — querying July must not find August's receivable.
	if recv, err := r.ListReceivables(context.Background(), cd(t, "2026-07-01"), cd(t, "2026-07-31")); err != nil || len(recv) != 0 {
		t.Fatalf("July receivables = %+v (err %v), want none", recv, err)
	}
	recv, err := r.ListReceivables(context.Background(), cd(t, "2026-08-01"), cd(t, "2026-08-31"))
	if err != nil || len(recv) != 1 || recv[0].Amount != 9700 {
		t.Fatalf("August receivables = %+v (err %v), want one of 9700", recv, err)
	}

	pays, err := r.ListPayments(context.Background(), cd(t, "2026-07-01"), cd(t, "2026-07-31"))
	if err != nil || len(pays) != 1 || pays[0].Amount != 9700 {
		t.Fatalf("payments = %+v (err %v), want one of 9700", pays, err)
	}
}

func TestListRangeIsInclusiveOfBothEnds(t *testing.T) {
	r, _ := newRepo(t, 0)
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-31"),
		Sales: []Sale{sale(t, "first", "2026-07-01", 100, 100), sale(t, "last", "2026-07-31", 200, 200)},
	})

	// The upper bound appends skHigh precisely so a sale on the "to" day, whose
	// SK carries a trailing id, still falls inside the range.
	got := listSaleIDs(t, r, "2026-07-01", "2026-07-31")
	if strings.Join(got, ",") != "first,last" {
		t.Fatalf("sales = %v, want both boundary days", got)
	}
}

func TestSaveIsIdempotent(t *testing.T) {
	r, tbl := newRepo(t, 0)
	result := ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales:       []Sale{sale(t, "A1", "2026-07-10", 10000, 9700)},
		Receivables: []ExpectedReceivable{receivable(t, "A1", "2026-08-09", 9700, 1)},
	}

	saveResult(t, r, result)
	after := tbl.Len()
	saveResult(t, r, result) // re-dropping the same envelope must converge

	if tbl.Len() != after {
		t.Fatalf("table has %d items after a re-import, want the same %d", tbl.Len(), after)
	}
	if got := listSaleIDs(t, r, "2026-07-01", "2026-07-31"); len(got) != 1 {
		t.Fatalf("sales after re-import = %v, want exactly one", got)
	}
}

func TestSaveReplacesOnlyItsOwnImport(t *testing.T) {
	r, _ := newRepo(t, 0)
	// Two imports of different source days, both writing sales in July.
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "day10", "2026-07-10", 100, 100)},
	})
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-11"),
		Sales: []Sale{sale(t, "day11", "2026-07-11", 200, 200)},
	})

	// Re-running only day 10, now with different content.
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "day10-corrigido", "2026-07-10", 150, 150)},
	})

	got := listSaleIDs(t, r, "2026-07-01", "2026-07-31")
	// day10's old sale is gone, its replacement is in, and day11 is untouched.
	if strings.Join(got, ",") != "day10-corrigido,day11" {
		t.Fatalf("sales = %v, want [day10-corrigido day11]", got)
	}
}

func TestSaveDeletesItemsDroppedFromAReimport(t *testing.T) {
	r, _ := newRepo(t, 0)
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "keep", "2026-07-10", 100, 100), sale(t, "drop", "2026-07-10", 200, 200)},
	})

	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "keep", "2026-07-10", 100, 100)},
	})

	got := listSaleIDs(t, r, "2026-07-01", "2026-07-31")
	if strings.Join(got, ",") != "keep" {
		t.Fatalf("sales = %v, want only [keep] — the dropped sale survived the replace", got)
	}
}

func TestSaveRejectsInvalidResultBeforeWriting(t *testing.T) {
	r, tbl := newRepo(t, 0)
	// A sale whose provider disagrees with the import header.
	bad := sale(t, "A1", "2026-07-10", 100, 100)
	bad.Provider = ProviderStone

	err := r.Save(context.Background(), ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"), Sales: []Sale{bad},
	})
	if err == nil {
		t.Fatal("expected Save to reject a provider mismatch")
	}
	if tbl.Len() != 0 {
		t.Fatal("a rejected import must not write anything")
	}
}

func TestSaveChunksLargeImportsIntoBatches(t *testing.T) {
	r, tbl := newRepo(t, 0)
	sales := make([]Sale, 0, 60)
	for i := range 60 {
		sales = append(sales, sale(t, "S"+string(rune('A'+i/26))+string(rune('a'+i%26)), "2026-07-10", 100, 100))
	}

	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"), Sales: sales,
	})

	// 60 items at DynamoDB's 25-per-call cap means three BatchWriteItem calls.
	if got := tbl.Calls("BatchWriteItem"); got != 3 {
		t.Fatalf("BatchWriteItem calls = %d, want 3", got)
	}
	if got := listSaleIDs(t, r, "2026-07-01", "2026-07-31"); len(got) != 60 {
		t.Fatalf("stored %d sales, want 60", len(got))
	}
}

func TestSaveRetriesUnprocessedItems(t *testing.T) {
	r, tbl := newRepo(t, 0)
	tbl.ThrottleBatches(2) // the first two batch calls come back fully unprocessed

	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "A1", "2026-07-10", 100, 100)},
	})

	if got := tbl.Calls("BatchWriteItem"); got != 3 {
		t.Fatalf("BatchWriteItem calls = %d, want 3 (two throttled, one that lands)", got)
	}
	if got := listSaleIDs(t, r, "2026-07-01", "2026-07-31"); len(got) != 1 {
		t.Fatalf("sales = %v, want the retried write to have landed", got)
	}
}

func TestSaveGivesUpAfterPersistentThrottling(t *testing.T) {
	r, tbl := newRepo(t, 0)
	tbl.ThrottleBatches(100) // never lets a batch through

	err := r.Save(context.Background(), ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "A1", "2026-07-10", 100, 100)},
	})
	if err == nil || !strings.Contains(err.Error(), "unprocessed") {
		t.Fatalf("error = %v, want it to report items still unprocessed", err)
	}
}

func TestSaveWritesIndexLastSoACrashIsRecoverable(t *testing.T) {
	r, tbl := newRepo(t, 0)
	ctx := context.Background()
	result := ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "A1", "2026-07-10", 100, 100)},
	}

	// Fail the index write: the data items are already in, but the index still
	// describes the previous (empty) version.
	tbl.FailNext("PutItem", errors.New("crash before index"))
	if err := r.Save(ctx, result); err == nil {
		t.Fatal("expected the failed index write to surface")
	}

	// Re-running the same envelope must converge on the correct state rather
	// than duplicating or orphaning anything.
	saveResult(t, r, result)
	if got := listSaleIDs(t, r, "2026-07-01", "2026-07-31"); strings.Join(got, ",") != "A1" {
		t.Fatalf("sales after recovery = %v, want exactly [A1]", got)
	}
}

func TestSavePropagatesIndexReadError(t *testing.T) {
	r, tbl := newRepo(t, 0)
	boom := errors.New("index read failed")
	tbl.FailNext("GetItem", boom)

	err := r.Save(context.Background(), ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "A1", "2026-07-10", 100, 100)},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
	if tbl.Len() != 0 {
		t.Fatal("Save must not write when it cannot read the previous key set")
	}
}

func TestSavePropagatesBatchWriteError(t *testing.T) {
	r, tbl := newRepo(t, 0)
	boom := errors.New("batch failed")
	tbl.FailNext("BatchWriteItem", boom)

	err := r.Save(context.Background(), ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales: []Sale{sale(t, "A1", "2026-07-10", 100, 100)},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestSameDayAnticipationKeepsEveryInstallment(t *testing.T) {
	r, _ := newRepo(t, 0)
	// One sale liquidating three installments on the same day: without the
	// parcela in the sort key these would collapse onto a single item.
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Payments: []Payment{
			payment(t, "A1", "2026-07-11", 100, 1),
			payment(t, "A1", "2026-07-11", 200, 2),
			payment(t, "A1", "2026-07-11", 300, 3),
		},
	})

	pays, err := r.ListPayments(context.Background(), cd(t, "2026-07-01"), cd(t, "2026-07-31"))
	if err != nil {
		t.Fatalf("list payments: %v", err)
	}
	if len(pays) != 3 {
		t.Fatalf("got %d payments, want all 3 same-day installments", len(pays))
	}
	var total int64
	for _, p := range pays {
		total += p.Amount
	}
	if total != 600 {
		t.Fatalf("total = %d, want 600", total)
	}
}

func TestListPaginatesThroughEveryPage(t *testing.T) {
	r, tbl := newRepo(t, 2) // force the queryRange paginator to loop
	sales := make([]Sale, 0, 7)
	for i := range 7 {
		sales = append(sales, sale(t, "S"+string(rune('a'+i)), "2026-07-10", 100, 100))
	}
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"), Sales: sales,
	})

	if got := listSaleIDs(t, r, "2026-07-01", "2026-07-31"); len(got) != 7 {
		t.Fatalf("got %d sales across pages, want 7", len(got))
	}
	if tbl.Calls("Query") < 4 {
		t.Fatalf("expected several Query calls for 7 items at 2 per page, got %d", tbl.Calls("Query"))
	}
}

func TestListPropagatesQueryError(t *testing.T) {
	r, tbl := newRepo(t, 0)
	ctx := context.Background()

	for name, call := range map[string]func() error{
		"sales":       func() error { _, err := r.ListSales(ctx, cd(t, "2026-07-01"), cd(t, "2026-07-31")); return err },
		"receivables": func() error { _, err := r.ListReceivables(ctx, cd(t, "2026-07-01"), cd(t, "2026-07-31")); return err },
		"payments":    func() error { _, err := r.ListPayments(ctx, cd(t, "2026-07-01"), cd(t, "2026-07-31")); return err },
	} {
		t.Run(name, func(t *testing.T) {
			boom := errors.New("query failed")
			tbl.FailNext("Query", boom)
			if err := call(); !errors.Is(err, boom) {
				t.Fatalf("error = %v, want it to wrap %v", err, boom)
			}
		})
	}
}

func TestListsAreScopedByItemPrefix(t *testing.T) {
	r, _ := newRepo(t, 0)
	ctx := context.Background()
	saveResult(t, r, ImportResult{
		Provider: ProviderPagBank, SourceDate: cd(t, "2026-07-10"),
		Sales:       []Sale{sale(t, "A1", "2026-07-10", 100, 100)},
		Receivables: []ExpectedReceivable{receivable(t, "A1", "2026-07-10", 100, 1)},
		Payments:    []Payment{payment(t, "A1", "2026-07-10", 100, 1)},
	})

	// All three item kinds sit on the same day in the same partition, so each
	// list must be separated by its SK prefix alone.
	from, to := cd(t, "2026-07-01"), cd(t, "2026-07-31")
	sales, _ := r.ListSales(ctx, from, to)
	recv, _ := r.ListReceivables(ctx, from, to)
	pays, _ := r.ListPayments(ctx, from, to)
	if len(sales) != 1 || len(recv) != 1 || len(pays) != 1 {
		t.Fatalf("got %d sales, %d receivables, %d payments — want 1 of each", len(sales), len(recv), len(pays))
	}
}
