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
	months := flag.Int("months", 12, "number of past months to generate data for")
	seed := flag.Int64("seed", 0, "RNG seed (0 = random)")
	flag.Parse()

	ctx := context.Background()
	store, err := pkgfinance.NewDynamoDBStore(ctx, *table, *endpoint)
	if err != nil {
		log.Fatalf("create store: %v", err)
	}

	seedVal := *seed
	if seedVal == 0 {
		seedVal = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seedVal))
	now := time.Now().UTC()
	count := 0

	for m := *months - 1; m >= 0; m-- {
		base := time.Date(now.Year(), now.Month()-time.Month(m), 1, 0, 0, 0, 0, time.UTC)
		count += seedMonth(ctx, store, *userID, base, m, *months, rng)
	}

	log.Printf("seeded %d entries for user %q across %d months (seed=%d)", count, *userID, *months, seedVal)
}

// seasonalMultiplier returns a factor that shifts revenue up or down based on
// the calendar month. January and February dip post-holidays; November and
// December peak on Black Friday and Christmas.
func seasonalMultiplier(month time.Month) float64 {
	switch month {
	case time.January:
		return 0.85
	case time.February:
		return 0.90
	case time.June:
		return 0.95
	case time.October:
		return 1.05
	case time.November:
		return 1.10
	case time.December:
		return 1.05
	default:
		return 1.0
	}
}

// growthFactor returns a multiplier that simulates steady MoM growth.
// monthIndex 0 = oldest month, monthCount-1 = current month.
func growthFactor(monthIndex, monthCount int) float64 {
	// ~3% growth per month, so oldest month is ~65% of current
	return 1.0 + float64(monthIndex)*0.03
}

func seedMonth(ctx context.Context, store pkgfinance.Store, userID string, base time.Time, monthIndex, monthCount int, rng *rand.Rand) int {
	count := 0
	year, month := base.Year(), base.Month()

	// Combined growth + seasonality factor applied to revenue.
	revFactor := growthFactor(monthIndex, monthCount) * seasonalMultiplier(month)

	save := func(e domain.FinancialEntry) {
		if err := store.SaveEntry(ctx, e); err != nil {
			log.Printf("warn: save entry: %v", err)
			return
		}
		count++
	}

	// --- Fixed monthly expenses ---

	// Folha de pagamento — day 5
	save(expense(userID, date(year, month, 5), randBetween(rng, 500000, 800000), "folha_pagamento", "Folha de Pagamento", "Farmácia Ltda", domain.PaymentStatusPaid))

	// Aluguel — day 10
	save(expense(userID, date(year, month, 10), randBetween(rng, 400000, 600000), "aluguel", "Aluguel", "Imobiliária Central", domain.PaymentStatusPaid))

	// Energia + água — day 8
	save(expense(userID, date(year, month, 8), randBetween(rng, 50000, 100000), "energia_agua", "Energia Elétrica / Água", "CEMIG / COPASA", domain.PaymentStatusPaid))

	// Telefone/internet — day 12
	save(expense(userID, date(year, month, 12), randBetween(rng, 20000, 40000), "telefone_internet", "Telefone / Internet", "Operadora", domain.PaymentStatusPaid))

	// Imposto — DARF dia 20
	save(expense(userID, date(year, month, 20), randBetween(rng, 200000, 400000), "impostos", "DARF Simples Nacional", "Receita Federal", domain.PaymentStatusPaid))

	// Cartão de crédito — day 15
	save(expense(userID, date(year, month, 15), randBetween(rng, 100000, 200000), "cartao_credito", "Fatura Cartão Corporativo", "Banco", domain.PaymentStatusPaid))

	// Empréstimo mensal — day 25
	save(expense(userID, date(year, month, 25), randBetween(rng, 80000, 150000), "emprestimo", "Parcela Empréstimo Banco", "Banco", domain.PaymentStatusPaid))

	// Manutenção — day 22 (new category)
	save(expense(userID, date(year, month, 22), randBetween(rng, 20000, 50000), "manutencao", "Manutenção Preventiva", "Manutenção Ltda", domain.PaymentStatusPaid))

	// --- Fornecedores (bi-weekly) ---
	distributors := []string{"Alfarma", "Profarma", "Coop"}
	for _, day := range []int{3, 17} {
		dist := distributors[rng.Intn(len(distributors))]
		save(expense(userID, date(year, month, day), randBetween(rng, 500000, 750000), "fornecedor_medicamentos", "Compra Distribuidora "+dist, "Distribuidora", domain.PaymentStatusPaid))
	}

	// Fornecedor geral (embalagens, etc.) — weekly
	for _, day := range []int{7, 14, 21, 28} {
		if day > daysInMonth(year, month) {
			continue
		}
		save(expense(userID, date(year, month, day), randBetween(rng, 50000, 150000), "fornecedor_geral", "Embalagens e Insumos", "Fornecedor Geral", domain.PaymentStatusPaid))
	}

	// --- Receitas: vendas diárias (weekdays only) ---
	// Base per-day range scaled by revFactor. Weekdays R$800-1400 base,
	// Saturdays half that.
	daysCount := daysInMonth(year, month)
	for day := 1; day <= daysCount; day++ {
		d := date(year, month, day)
		weekday := d.Weekday()
		if weekday == time.Saturday {
			amt := int64(float64(randBetween(rng, 40000, 70000)) * revFactor)
			save(income(userID, d, amt, "venda_balcao", "Venda Balcão - Sábado", domain.OriginVenda))
			continue
		}
		if weekday == time.Sunday {
			continue // closed
		}
		amt := int64(float64(randBetween(rng, 80000, 140000)) * revFactor)
		save(income(userID, d, amt, "venda_balcao", "Venda Balcão", domain.OriginVenda))
	}

	// Convênio — 2-3 repasses per month
	lastDay := daysInMonth(year, month)
	save(income(userID, date(year, month, lastDay), int64(float64(randBetween(rng, 150000, 250000))*revFactor), "convenio", "Repasse Convênio", domain.OriginVenda))
	if lastDay > 15 {
		save(income(userID, date(year, month, 15), int64(float64(randBetween(rng, 100000, 180000))*revFactor), "convenio", "Repasse Convênio Unimed", domain.OriginVenda))
	}
	if lastDay > 20 {
		save(income(userID, date(year, month, 20), int64(float64(randBetween(rng, 80000, 150000))*revFactor), "convenio", "Repasse Convênio Amil", domain.OriginVenda))
	}

	// Delivery / tele-entrega — 2-3 per month
	for _, day := range []int{10, 20} {
		if day <= daysCount {
			save(income(userID, date(year, month, day), int64(float64(randBetween(rng, 50000, 100000))*revFactor), "delivery", "Delivery / Tele-entrega", domain.OriginVenda))
		}
	}

	// --- Entradas que NÃO são faturamento ---
	// Bonificação de laboratório — every month
	save(income(userID, date(year, month, 8), randBetween(rng, 20000, 60000), "outros_receitas", "Bonificação Laboratório", domain.OriginOutros))

	// Empréstimo de capital de giro — only months 1 and 7 (oldest and mid-point)
	if monthIndex == 0 || monthIndex == 6 {
		save(income(userID, date(year, month, 12), randBetween(rng, 1000000, 1500000), "outros_receitas", "Empréstimo de capital de giro", domain.OriginEmprestimo))
	}

	// Aporte de sócio — only month 1
	if monthIndex == 0 {
		save(income(userID, date(year, month, 18), randBetween(rng, 300000, 500000), "outros_receitas", "Aporte de sócio", domain.OriginAporteSocio))
	}

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
			income(userID, now, randBetween(rng, 150000, 250000), "convenio", "Repasse Convênio (a receber)", domain.OriginVenda),
			nextConvenio,
		))

		// Venda no crediário atravessando o mês
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
		RevenueTarget: 2800000, // R$ 28.000,00
		ExpenseTarget: 2200000, // R$ 22.000,00
	}
	if err := store.SaveGoal(ctx, goal); err != nil {
		log.Printf("warn: seed goal: %v", err)
	} else {
		log.Printf("goal set for %s: faturamento=R$%.0f teto=R$%.0f", goal.Month, float64(goal.RevenueTarget)/100, float64(goal.ExpenseTarget)/100)
	}
}

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
