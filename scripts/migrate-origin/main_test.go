package main

import (
	"testing"

	"github.com/emerson/emerbot/packages/domain"
)

// The backfill's whole promise is that no figure moves on the day it runs: the
// origin it writes must reproduce the pre-migration faturamento exactly. If
// this test and domain.IsRevenue's shim ever disagree, the migration changes
// history — which is the one thing it must not do.
func TestOriginForLegacyMatchesTheShim(t *testing.T) {
	for _, category := range []string{
		"venda_balcao", "convenio", "delivery", "outros_receitas",
		"venda_online", // a category the user made up: counted before, counts now
	} {
		legacy := domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: category}
		migrated := domain.FinancialEntry{
			Type:     domain.EntryTypeIncome,
			Category: category,
			Origin:   originForLegacy(category),
		}
		if domain.IsRevenue(legacy) != domain.IsRevenue(migrated) {
			t.Errorf("category %q: faturamento flips from %v to %v across the migration",
				category, domain.IsRevenue(legacy), domain.IsRevenue(migrated))
		}
	}
}

func TestPlanFor(t *testing.T) {
	const pk = "USER#u1"

	tests := []struct {
		name string
		row  entryRow
		want plan
	}{
		{
			name: "paid sale needs an origin and a cash-date index entry",
			row: entryRow{
				PK: pk, EntryID: "e1", Type: "income", Category: "venda_balcao",
				PaymentStatus: "paid", PaymentDate: "2026-07-20",
			},
			want: plan{
				setOrigin: true, origin: domain.OriginVenda,
				rewriteGSI1: true, gsi1PK: pk, gsi1SK: "2026-07-20#e1",
			},
		},
		{
			name: "outros_receitas becomes outros, preserving today's faturamento",
			row: entryRow{
				PK: pk, EntryID: "e2", Type: "income", Category: "outros_receitas",
				PaymentStatus: "paid", PaymentDate: "2026-07-20",
			},
			want: plan{
				setOrigin: true, origin: domain.OriginOutros,
				rewriteGSI1: true, gsi1PK: pk, gsi1SK: "2026-07-20#e2",
			},
		},
		{
			name: "pending entry is removed from the index, not blanked",
			row: entryRow{
				PK: pk, EntryID: "e3", Type: "income", Category: "convenio",
				PaymentStatus: "pending",
				// The stale category-shaped value the old scheme wrote.
				GSI1PK: pk, GSI1SK: "convenio#2026-07-14",
			},
			want: plan{
				setOrigin: true, origin: domain.OriginVenda,
				rewriteGSI1: true, gsi1PK: "", gsi1SK: "",
			},
		},
		{
			name: "expenses get no origin but still need the cash index",
			row: entryRow{
				PK: pk, EntryID: "e4", Type: "expense", Category: "aluguel",
				PaymentStatus: "paid", PaymentDate: "2026-07-09",
			},
			want: plan{rewriteGSI1: true, gsi1PK: pk, gsi1SK: "2026-07-09#e4"},
		},
		{
			name: "an entry already carrying an origin is left alone",
			row: entryRow{
				PK: pk, EntryID: "e5", Type: "income", Category: "venda_balcao",
				Origin: string(domain.OriginEmprestimo), PaymentStatus: "paid",
				PaymentDate: "2026-07-20", GSI1PK: pk, GSI1SK: "2026-07-20#e5",
			},
			want: plan{},
		},
		{
			name: "legacy RFC3339 payment date is truncated to a calendar day",
			row: entryRow{
				PK: pk, EntryID: "e6", Type: "expense", Category: "aluguel",
				PaymentStatus: "paid", PaymentDate: "2026-07-09T00:00:00Z",
			},
			want: plan{rewriteGSI1: true, gsi1PK: pk, gsi1SK: "2026-07-09#e6"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := planFor(tc.row); got != tc.want {
				t.Errorf("planFor() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Re-running the migration must be a no-op — the runbook says to dry-run, then
// run, and a half-finished run has to be safe to resume.
func TestPlanForIsIdempotent(t *testing.T) {
	row := entryRow{
		PK: "USER#u1", EntryID: "e1", Type: "income", Category: "venda_balcao",
		PaymentStatus: "paid", PaymentDate: "2026-07-20",
	}

	p := planFor(row)
	row.Origin = string(p.origin)
	row.GSI1PK, row.GSI1SK = p.gsi1PK, p.gsi1SK

	if second := planFor(row); !second.empty() {
		t.Errorf("second pass still wants changes: %+v", second)
	}
}
