import {
  Wallet, TrendingUp, TrendingDown, Clock, Banknote, Target,
} from 'lucide-react'
import {
  useMonthlySummary, useEntries, useMarkPaidMutation, useDeleteEntryMutation,
} from '../api/queries'
import { useMonthlyAnalysis } from '../hooks/useMonthlyAnalysis'
import KpiCard, { KpiCardContent, toneVar } from '../components/KpiCard'
import GoalCard from '../components/GoalCard'
import CashFlowChart from '../components/CashFlowChart'
import IncomeExpenseChart from '../components/IncomeExpenseChart'
import CategoryBars from '../components/CategoryBars'
import TransactionsTable from '../components/TransactionsTable'
import WorstMonth from './WorstMonth';
import MonthlyExpent from './MonthlyExpent';
import ToPayToday from './ToPayToday';
import { formatBRL } from '@/lib/format'
import { currentMonthKey } from '@/lib/entries'
import { currentMonthRange } from '@/lib/period'
import { projectionBasisNote, projectionTone } from '@/lib/projection'
import type { YearMonth } from '@/api/types'

function ExpenseTotal() {
  const currentMonth = currentMonthKey()
  const summaryQuery = useMonthlySummary(currentMonth)
  const summary = summaryQuery.data ?? null

  return (
    <KpiCard
      tone="negative"
      isLoading={summaryQuery.isLoading}
      isError={summaryQuery.isError}
      errorMessage="Erro ao carregar despesas"
      className="min-h-26"
    >
      <KpiCardContent icon={TrendingDown} tone="negative">
        <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Total Despesas</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.negative }}>
          {formatBRL(summary?.TotalExpense ?? 0)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Este mês</p>
      </KpiCardContent>
    </KpiCard>
  )
}

// Where the month's balance lands, from the backend's projection — the same
// figure the Analysis page and the WhatsApp bot quote.
//
// This card used to show summary.ExpectedBalance: the month's whole expected
// inflow minus its whole expense, which on any early day of a month is every
// bill already booked against sales nobody has made yet. That is the reading
// ADR-017 found reporting a critical month every 1st, and the analysis stopped
// using it — the dashboard kept it, because it read the summary directly.
function CashProjectionCard() {
  const currentMonth = currentMonthKey() as YearMonth
  const analysisQuery = useMonthlyAnalysis(currentMonth)
  const cash = analysisQuery.data?.cashPosition ?? null
  const balance = cash?.endOfMonthProjection ?? 0
  const balanceTone = balance >= 0 ? 'positive' : 'negative'

  return (
    <KpiCard
      tone={balanceTone}
      isLoading={analysisQuery.isLoading}
      isError={analysisQuery.isError}
      errorMessage="Erro ao carregar projeção"
      className="min-h-26"
    >
      <KpiCardContent icon={Wallet} tone={balanceTone}>
        <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Projeção Fim do Mês</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar[balanceTone] }}>
          {formatBRL(balance)}
        </p>
        {/* What the projection assumes, said out loud — the days ahead are
            credited with an ordinary day's receipts, and without any trading
            history there is nothing to credit them with. */}
        <p className="mt-1 text-xs text-muted-foreground">
          {cash?.expectsReceipts === false
            ? 'Só o que já está lançado — sem histórico para projetar'
            : 'Contando o recebimento médio dos dias que faltam'}
        </p>
      </KpiCardContent>
    </KpiCard>
  )
}

// Where the month's faturamento lands against its goal. The Analysis page has
// carried this since the projection moved to Go; the dashboard had the goal bar
// (GoalCard) showing where the month *is* and nothing saying where it ends up.
function RevenueProjectionCard() {
  const currentMonth = currentMonthKey() as YearMonth
  const analysisQuery = useMonthlyAnalysis(currentMonth)
  const projection = analysisQuery.data?.projection ?? null

  return (
    <KpiCard
      tone="info"
      isLoading={analysisQuery.isLoading}
      isError={analysisQuery.isError}
      errorMessage="Erro ao carregar projeção de faturamento"
      className="min-h-26"
    >
      <KpiCardContent icon={Target} tone="info">
        <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Faturamento Projetado</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.info }}>
          {formatBRL(projection?.projected ?? 0)}
        </p>
        {/* The verdict and the percentage both come from the backend. Rounding
            one here beside a colour decided there is how a screen once showed
            "97% da meta" in red under a green "Ritmo suficiente". */}
        {projection && projection.target > 0 ? (
          <p className={`mt-1 text-xs font-medium ${projectionTone[projection.status]}`}>
            {Math.round(projection.coverage * 100)}% da meta
          </p>
        ) : (
          <p className="mt-1 text-xs text-muted-foreground">Sem meta definida para o mês</p>
        )}
        {projection && projectionBasisNote[projection.basis] && (
          <p className="mt-1 text-xs text-muted-foreground">{projectionBasisNote[projection.basis]}</p>
        )}
      </KpiCardContent>
    </KpiCard>
  )
}

// Faturamento and entradas de caixa are two cards rather than one "Receitas"
// total because they answer different questions and routinely disagree: a loan
// is cash and not a sale, an unpaid crediário sale is a sale and not yet cash.
// One number labelled "Receitas" hid both facts.
function RevenueTotal() {
  const currentMonth = currentMonthKey()
  const summaryQuery = useMonthlySummary(currentMonth)
  const summary = summaryQuery.data ?? null

  return (
    <KpiCard
      tone="positive"
      isLoading={summaryQuery.isLoading}
      isError={summaryQuery.isError}
      errorMessage="Erro ao carregar faturamento"
      className="min-h-26"
    >
      <KpiCardContent icon={TrendingUp} tone="positive">
        <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Faturamento</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.positive }}>
          {formatBRL(summary?.TotalRevenue ?? 0)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Vendas deste mês</p>
      </KpiCardContent>
    </KpiCard>
  )
}

function CashInTotal() {
  const currentMonth = currentMonthKey()
  const summaryQuery = useMonthlySummary(currentMonth)
  const summary = summaryQuery.data ?? null

  return (
    <KpiCard
      tone="info"
      isLoading={summaryQuery.isLoading}
      isError={summaryQuery.isError}
      errorMessage="Erro ao carregar entradas de caixa"
      className="min-h-26"
    >
      <KpiCardContent icon={Banknote} tone="info">
        <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Entradas de Caixa</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.info }}>
          {formatBRL(summary?.TotalCashIn ?? 0)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Todo dinheiro recebido</p>
      </KpiCardContent>
    </KpiCard>
  )
}

function Receivables() {
  const { month: currentMonth, firstDay, lastDay } = currentMonthRange()

  const summaryQuery = useMonthlySummary(currentMonth)
  const entriesQuery = useEntries(firstDay, lastDay)
  const entries = entriesQuery.data?.entries ?? []

  const totalReceivable = entries
    .filter(e => e.Type === 'income' && e.PaymentStatus === 'pending')
    .reduce((sum, e) => sum + e.Amount, 0)

  return (
    <KpiCard
      tone="info"
      isLoading={summaryQuery.isLoading || entriesQuery.isLoading}
      isError={summaryQuery.isError || entriesQuery.isError}
      errorMessage="Erro ao carregar recebíveis"
      className="min-h-26"
    >
      <KpiCardContent icon={Clock} tone="info">
        <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">A Receber</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.info }}>
          {formatBRL(totalReceivable)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Pendente</p>
      </KpiCardContent>
    </KpiCard>
  )
}

export default function Dashboard() {
  const { month: currentMonth, firstDay, lastDay } = currentMonthRange()
  // The chart draws the backend's projected curve, not the booked one — see
  // CashFlowChart. GoalCard already asks for this analysis, so it costs nothing.
  const analysisQuery = useMonthlyAnalysis(currentMonth as YearMonth)
  const entriesQuery = useEntries(firstDay, lastDay)
  const markPaid = useMarkPaidMutation()
  const deleteEntry = useDeleteEntryMutation()

  const forecast = analysisQuery.data?.cashPosition.forecast ?? []
  const entries = entriesQuery.data?.entries ?? []

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Painel de Controle</h1>
        <p className="mt-1 text-muted-foreground">Visão geral financeira do estabelecimento</p>
      </div>

      <div className="grid grid-cols-1 items-stretch gap-4 sm:grid-cols-2 md:grid-cols-3">
        <RevenueTotal />
        <CashInTotal />
        <ExpenseTotal />
        <CashProjectionCard />
        <RevenueProjectionCard />
        <Receivables />
        <ToPayToday />
        <WorstMonth />
        <GoalCard />
      </div>

      <CashFlowChart data={forecast} />
      <IncomeExpenseChart />

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <MonthlyExpent />
        <CategoryBars />
      </div>

      <TransactionsTable
        entries={entries}
        isLoading={entriesQuery.isLoading}
        onMarkPaid={entry => markPaid.mutate(entry)}
        onDelete={entry => deleteEntry.mutate(entry)}
      />
    </div>
  )
}
