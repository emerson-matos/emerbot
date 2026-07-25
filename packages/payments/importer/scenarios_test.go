package importer

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/payments"
	"github.com/emerson/emerbot/packages/payments/pagbank"
)

// These run the recorded PagBank scenarios through the whole import pipeline —
// assemble, peek the provider, parse, validate, persist — using the same
// Importer the Lambda and the pagbank-import script build. Only the repository
// is swapped for the in-memory one, so everything above the storage client is
// exercised exactly as it runs in production.

// broadRange is a window wide enough to hold every scenario's dates plus any
// rebase target, so a listing assertion never fails merely for querying too
// narrow a period.
type dateRange struct{ from, to domain.CalendarDate }

func broadRange(t *testing.T) dateRange {
	t.Helper()
	from, err := domain.ParseCalendarDate("2000-01-01")
	if err != nil {
		t.Fatalf("parse from: %v", err)
	}
	to, err := domain.ParseCalendarDate("2099-12-31")
	if err != nil {
		t.Fatalf("parse to: %v", err)
	}
	return dateRange{from: from, to: to}
}

func scenarioDirs(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "cenarios")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("recorded scenarios not present: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	return dirs
}

func assembleScenario(t *testing.T, dir string, opts pagbank.AssembleOptions) []byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var extracts []pagbank.Extract
	for _, name := range names {
		kind, ok := pagbank.ClassifyFile(name)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		extracts = append(extracts, pagbank.Extract{Kind: kind, Name: name, Data: data})
	}

	raw, err := pagbank.Assemble(extracts, opts)
	if err != nil {
		t.Fatalf("assemble %s: %v", dir, err)
	}
	return raw
}

func TestImportRecordedScenariosEndToEnd(t *testing.T) {
	ctx := context.Background()

	for _, dir := range scenarioDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			raw := assembleScenario(t, dir, pagbank.AssembleOptions{})
			repo := payments.NewInMemoryRepository()

			if err := New(repo).ProcessRaw(ctx, raw); err != nil {
				t.Fatalf("ProcessRaw: %v", err)
			}

			wide := broadRange(t)
			sales, err := repo.ListSales(ctx, wide.from, wide.to)
			if err != nil {
				t.Fatalf("ListSales: %v", err)
			}
			pays, err := repo.ListPayments(ctx, wide.from, wide.to)
			if err != nil {
				t.Fatalf("ListPayments: %v", err)
			}
			if len(sales) == 0 && len(pays) == 0 {
				t.Fatal("scenario imported nothing")
			}

			// A sale's fee must be exactly what it lost between gross and net;
			// a mismatch means the parser mapped the wrong column.
			for _, s := range sales {
				if s.FeeAmount != s.GrossAmount-s.NetAmount {
					t.Errorf("sale %s: fee %d != gross %d - net %d",
						s.ID, s.FeeAmount, s.GrossAmount, s.NetAmount)
				}
			}
		})
	}
}

// Re-importing the same envelope must leave the store exactly as it was: this is
// the property the whole replace-by-source-day design exists to provide, and the
// recovery procedure ("re-drop the object") depends on it.
func TestReimportingAScenarioIsIdempotent(t *testing.T) {
	ctx := context.Background()

	for _, dir := range scenarioDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			raw := assembleScenario(t, dir, pagbank.AssembleOptions{})
			repo := payments.NewInMemoryRepository()
			imp := New(repo)

			if err := imp.ProcessRaw(ctx, raw); err != nil {
				t.Fatalf("first import: %v", err)
			}
			wide := broadRange(t)
			salesBefore, _ := repo.ListSales(ctx, wide.from, wide.to)
			recvBefore, _ := repo.ListReceivables(ctx, wide.from, wide.to)
			paysBefore, _ := repo.ListPayments(ctx, wide.from, wide.to)

			if err := imp.ProcessRaw(ctx, raw); err != nil {
				t.Fatalf("second import: %v", err)
			}
			salesAfter, _ := repo.ListSales(ctx, wide.from, wide.to)
			recvAfter, _ := repo.ListReceivables(ctx, wide.from, wide.to)
			paysAfter, _ := repo.ListPayments(ctx, wide.from, wide.to)

			if len(salesAfter) != len(salesBefore) ||
				len(recvAfter) != len(recvBefore) ||
				len(paysAfter) != len(paysBefore) {
				t.Errorf("re-import changed the store: sales %d→%d, receivables %d→%d, payments %d→%d",
					len(salesBefore), len(salesAfter),
					len(recvBefore), len(recvAfter),
					len(paysBefore), len(paysAfter))
			}
		})
	}
}

// Rebasing is what makes the scenarios usable in the demo, so it has to produce
// something importable too — not just something that assembles.
func TestRebasedScenarioStillImports(t *testing.T) {
	ctx := context.Background()
	dirs := scenarioDirs(t)
	if len(dirs) == 0 {
		t.Skip("no scenarios")
	}

	raw := assembleScenario(t, dirs[0], pagbank.AssembleOptions{RebaseTo: "2026-03"})
	repo := payments.NewInMemoryRepository()
	if err := New(repo).ProcessRaw(ctx, raw); err != nil {
		t.Fatalf("ProcessRaw on rebased scenario: %v", err)
	}

	wide := broadRange(t)
	sales, _ := repo.ListSales(ctx, wide.from, wide.to)
	pays, _ := repo.ListPayments(ctx, wide.from, wide.to)
	if len(sales) == 0 && len(pays) == 0 {
		t.Fatal("rebased scenario imported nothing")
	}
	for _, s := range sales {
		if y := s.SaleDate.Year(); y != 2026 {
			t.Errorf("sale %s kept year %d; rebase should have moved it to 2026", s.ID, y)
		}
	}
}
