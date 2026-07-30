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
	userID := flag.String("user-id", shared.FinanceLedgerID, "user ID to seed data for")
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
			save(income(userID, d, randBetween(rng, 60000, 120000), "venda_balcao", "Venda Balcão - Sábado", domain.OriginVenda))
			continue
		}
		if weekday == time.Sunday {
			continue // closed
		}
		save(income(userID, d, randBetween(rng, 120000, 350000), "venda_balcao", "Venda Balcão", domain.OriginVenda))
	}

	// Convênio (monthly reimbursement — 30th or last day)
	lastDay := daysInMonth(year, month)
	save(income(userID, date(year, month, lastDay), randBetween(rng, 800000, 1500000), "convenio", "Repasse Convênio", domain.OriginVenda))

	// --- Receitas avulsas no meio do mês ---

	// Convênio adicional — dia 15 (ex.: Unimed)
	save(income(userID, date(year, month, 15), randBetween(rng, 400000, 700000), "convenio", "Repasse Convênio Unimed", domain.OriginVenda))

	// Delivery / retirada — dias 10 e 20
	for _, day := range []int{10, 20} {
		save(income(userID, date(year, month, day), randBetween(rng, 80000, 250000), "delivery", "Delivery / Tele-entrega", domain.OriginVenda))
	}

	// --- Entradas que NÃO são faturamento ---
	// Sem elas os dois números do dashboard ficam idênticos e a separação entre
	// faturamento e entradas de caixa não aparece na demo.

	// Bonificação de laboratório — dinheiro que entrou sem venda.
	save(income(userID, date(year, month, 8), randBetween(rng, 50000, 150000), "outros_receitas", "Bonificação Laboratório", domain.OriginOutros))
	// Empréstimo — o caso que originou toda a separação: entra no caixa, não é
	// faturamento e não pode mexer na meta.
	save(income(userID, date(year, month, 12), randBetween(rng, 2000000, 3000000), "outros_receitas", "Empréstimo de capital de giro", domain.OriginEmprestimo))
	// Aporte de sócio — mesma história.
	save(income(userID, date(year, month, 18), randBetween(rng, 500000, 900000), "outros_receitas", "Aporte de sócio", domain.OriginAporteSocio))

	// --- Pending items for the current/future month only ---
	now := time.Now().UTC()
	if base.Year() == now.Year() && base.Month() == now.Month() {
		nextDue := time.Date(year, month+1, 3, 0, 0, 0, 0, time.UTC)
		pending := expense(userID, now, randBetween(rng, 1500000, 2000000), "fornecedor_medicamentos", "Compra Distribuidora (a pagar)", "Distribuidora", domain.PaymentStatusPending)
		dueDate := domain.NewCalendarDate(nextDue)
		pending.DueDate = &dueDate
		save(pending)

		nextConvenio := time.Date(year, month, lastDay, 0, 0, 0, 0, time.UTC)
		save(pendingIncome(
			income(userID, now, randBetween(rng, 800000, 1500000), "convenio", "Repasse Convênio (a receber)", domain.OriginVenda),
			nextConvenio,
		))

		// Venda no crediário atravessando o mês: vendida hoje, vence no mês que
		// vem. É o caso que separa as duas bases de data — tem de aparecer no
		// faturamento *deste* mês e em entrada de caixa de nenhum mês, até ser
		// paga. Sem ela a demo não exercita a diferença.
		save(pendingIncome(
			income(userID, now, 450000, "venda_balcao", "Venda no crediário (recebe mês que vem)", domain.OriginVenda),
			nextDue,
		))

		// A pagar hoje
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		save(pendingDue(
			expense(userID, now, 189700, "fornecedor_medicamentos", "Duplicata Distribuidora Alfarma", "Distribuidora", domain.PaymentStatusPending),
			today,
		))
		save(pendingDue(
			expense(userID, now, 54300, "fornecedor_geral", "Embalagens e Insumos (vencimento hoje)", "Fornecedor Geral", domain.PaymentStatusPending),
			today,
		))
		save(pendingIncome(
			income(userID, now, 312000, "convenio", "Convênio Unimed (a receber hoje)", domain.OriginVenda),
			today,
		))
	}

	// Goal do mês
	seedMonthGoal(ctx, store, userID, base)

	return count
}

func seedMonthGoal(ctx context.Context, store pkgfinance.Store, userID string, base time.Time) {
	goal := domain.Goal{
		UserID:        userID,
		Month:         base.Format("2006-01"),
		RevenueTarget: 80000000, // R$ 80.000,00
		ExpenseTarget: 60000000, // R$ 60.000,00
	}
	if err := store.SaveGoal(ctx, goal); err != nil {
		log.Printf("warn: seed goal: %v", err)
	} else {
		log.Printf("goal set for %s: faturamento=R$%.0f teto=R$%.0f", goal.Month, float64(goal.RevenueTarget)/100, float64(goal.ExpenseTarget)/100)
	}
}

// pendingIncome turns a paid inflow into a receivable due on the given day.
// It clears PaymentDate, which income() sets: a pending entry with a payment
// date fails domain.Validate, and the open-coded versions of this that the
// seed used to carry left it behind.
func pendingIncome(e domain.FinancialEntry, due time.Time) domain.FinancialEntry {
	e.PaymentStatus = domain.PaymentStatusPending
	e.PaymentDate = nil
	d := domain.NewCalendarDate(due)
	e.DueDate = &d
	return e
}

func pendingDue(e domain.FinancialEntry, due time.Time) domain.FinancialEntry {
	e.PaymentStatus = domain.PaymentStatusPending
	d := domain.NewCalendarDate(due)
	e.DueDate = &d
	return e
}

func expense(userID string, d time.Time, amount int64, cat, desc, supplier string, status domain.PaymentStatus) domain.FinancialEntry {
	now := time.Now().UTC()
	date := domain.NewCalendarDate(d)
	e := domain.FinancialEntry{
		UserID:          userID,
		EntryID:         domain.EntryID(uuid.New().String()),
		TransactionDate: date,
		Amount:          amount,
		Category:        cat,
		Type:            domain.EntryTypeExpense,
		Description:     desc,
		Supplier:        supplier,
		PaymentStatus:   status,
		Source:          domain.SourceManual,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if status == domain.PaymentStatusPaid {
		e.PaymentDate = &date
	}
	return e
}

// income builds a paid inflow. origin decides whether it is faturamento: only
// domain.OriginVenda is, so the loan and the aporte seeded below show up in
// entradas de caixa and not in the sales figures. Without at least one of each
// the demo's two headline numbers are identical and the split is invisible.
func income(userID string, d time.Time, amount int64, cat, desc string, origin domain.IncomeOrigin) domain.FinancialEntry {
	now := time.Now().UTC()
	date := domain.NewCalendarDate(d)
	return domain.FinancialEntry{
		UserID:          userID,
		EntryID:         domain.EntryID(uuid.New().String()),
		TransactionDate: date,
		Amount:          amount,
		Category:        cat,
		Type:            domain.EntryTypeIncome,
		Description:     desc,
		PaymentStatus:   domain.PaymentStatusPaid,
		PaymentDate:     &date,
		Source:          domain.SourceManual,
		Origin:          origin,
		CreatedAt:       now,
		UpdatedAt:       now,
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
