package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	endpoint := flag.String("endpoint", shared.Getenv("DYNAMODB_ENDPOINT", "http://localhost:8000"), "DynamoDB endpoint")
	table := flag.String("table", shared.Getenv("FINANCIAL_ENTRIES_TABLE", "emerbot-local-financial-entries"), "financial entries table name")
	userID := flag.String("user-id", "pai", "user ID to seed data for")
	months := flag.Int("months", 3, "number of past months to generate data for")
	flag.Parse()

	ctx := context.Background()
	store, err := pkgfinance.NewDynamoDBStore(ctx, *table, *endpoint)
	if err != nil {
		log.Fatalf("create store: %v", err)
	}

	rng := rand.New(rand.NewSource(42)) // deterministic seed for reproducibility
	now := time.Now().UTC()
	count := 0

	for m := *months - 1; m >= 0; m-- {
		base := time.Date(now.Year(), now.Month()-time.Month(m), 1, 0, 0, 0, 0, time.UTC)
		count += seedMonth(ctx, store, *userID, base, rng)
	}

	log.Printf("seeded %d entries for user %q across %d months", count, *userID, *months)
}

func seedMonth(ctx context.Context, store pkgfinance.Store, userID string, base time.Time, rng *rand.Rand) int {
	count := 0
	year, month := base.Year(), base.Month()

	save := func(e domain.FinancialEntry) {
		if err := store.SaveEntry(ctx, e); err != nil {
			log.Printf("warn: save entry: %v", err)
			return
		}
		count++
	}

	// --- Fixed monthly expenses ---

	// Folha de pagamento — day 5
	save(expense(userID, date(year, month, 5), randBetween(rng, 800000, 1200000), "folha_pagamento", "Folha de Pagamento", "Farmácia Ltda", domain.PaymentStatusPaid))

	// Aluguel — day 10
	save(expense(userID, date(year, month, 10), 350000, "aluguel", "Aluguel", "Imobiliária Central", domain.PaymentStatusPaid))

	// Energia + água — day 8
	save(expense(userID, date(year, month, 8), randBetween(rng, 80000, 120000), "energia_agua", "Energia Elétrica / Água", "CEMIG / COPASA", domain.PaymentStatusPaid))

	// Telefone/internet — day 12
	save(expense(userID, date(year, month, 12), 35000, "telefone_internet", "Telefone / Internet", "Operadora", domain.PaymentStatusPaid))

	// Imposto — DARF dia 20
	save(expense(userID, date(year, month, 20), randBetween(rng, 150000, 400000), "impostos", "DARF Simples Nacional", "Receita Federal", domain.PaymentStatusPaid))

	// Cartão de crédito — day 15
	save(expense(userID, date(year, month, 15), randBetween(rng, 200000, 500000), "cartao_credito", "Fatura Cartão Corporativo", "Banco", domain.PaymentStatusPaid))

	// --- Fornecedores (bi-weekly) ---
	distributors := []string{"Alfarma", "Profarma", "Coop"}
	for _, day := range []int{3, 17} {
		dist := distributors[rng.Intn(len(distributors))]
		save(expense(userID, date(year, month, day), randBetween(rng, 1500000, 2500000), "fornecedor_medicamentos", "Compra Distribuidora "+dist, "Distribuidora", domain.PaymentStatusPaid))
	}

	// Fornecedor geral (embalagens, etc.) — weekly
	for _, day := range []int{7, 14, 21, 28} {
		if day > daysInMonth(year, month) {
			continue
		}
		save(expense(userID, date(year, month, day), randBetween(rng, 50000, 200000), "fornecedor_geral", "Embalagens e Insumos", "Fornecedor Geral", domain.PaymentStatusPaid))
	}

	// Empréstimo mensal — day 25 (if this is within recent 6 months)
	save(expense(userID, date(year, month, 25), 120000, "emprestimo", "Parcela Empréstimo Banco", "Banco", domain.PaymentStatusPaid))

	// --- Receitas: vendas diárias (weekdays only) ---
	daysCount := daysInMonth(year, month)
	for day := 1; day <= daysCount; day++ {
		d := date(year, month, day)
		weekday := d.Weekday()
		if weekday == time.Saturday {
			// Half day on Saturdays
			save(income(userID, d, randBetween(rng, 60000, 120000), "venda_balcao", "Venda Balcão - Sábado"))
			continue
		}
		if weekday == time.Sunday {
			continue // closed
		}
		save(income(userID, d, randBetween(rng, 120000, 350000), "venda_balcao", "Venda Balcão"))
	}

	// Convênio (monthly reimbursement — 30th or last day)
	lastDay := daysInMonth(year, month)
	save(income(userID, date(year, month, lastDay), randBetween(rng, 800000, 1500000), "convenio", "Repasse Convênio"))

	// --- Pending items for the current/future month only ---
	now := time.Now().UTC()
	if base.Year() == now.Year() && base.Month() == now.Month() {
		// A pagar: próxima fatura de fornecedor
		nextDue := time.Date(year, month+1, 3, 0, 0, 0, 0, time.UTC)
		pending := expense(userID, now, randBetween(rng, 1500000, 2000000), "fornecedor_medicamentos", "Compra Distribuidora (a pagar)", "Distribuidora", domain.PaymentStatusPending)
		pending.DueDate = &nextDue
		save(pending)

		// A receber: convênio do mês atual
		nextConvenio := time.Date(year, month, lastDay, 0, 0, 0, 0, time.UTC)
		rec := income(userID, now, randBetween(rng, 800000, 1500000), "convenio", "Repasse Convênio (a receber)")
		rec.PaymentStatus = domain.PaymentStatusPending
		rec.DueDate = &nextConvenio
		save(rec)
	}

	return count
}

func expense(userID string, d time.Time, amount int64, cat, desc, supplier string, status domain.PaymentStatus) domain.FinancialEntry {
	now := time.Now().UTC()
	return domain.FinancialEntry{
		UserID:        userID,
		EntryID:       uuid.New().String(),
		Date:          d,
		Amount:        amount,
		Category:      cat,
		Type:          domain.EntryTypeExpense,
		Description:   desc,
		Supplier:      supplier,
		PaymentStatus: status,
		Source:        "seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func income(userID string, d time.Time, amount int64, cat, desc string) domain.FinancialEntry {
	now := time.Now().UTC()
	return domain.FinancialEntry{
		UserID:        userID,
		EntryID:       uuid.New().String(),
		Date:          d,
		Amount:        amount,
		Category:      cat,
		Type:          domain.EntryTypeIncome,
		Description:   desc,
		PaymentStatus: domain.PaymentStatusPaid,
		Source:        "seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func date(year int, month time.Month, day int) time.Time {
	max := daysInMonth(year, month)
	if day > max {
		day = max
	}
	return time.Date(year, month, day, 9, 0, 0, 0, time.UTC)
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func randBetween(rng *rand.Rand, min, max int64) int64 {
	return min + rng.Int63n(max-min)
}
