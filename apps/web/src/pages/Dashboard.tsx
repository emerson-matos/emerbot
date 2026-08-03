import { format } from 'date-fns'
import {
  Wallet, TrendingUp, TrendingDown, Clock, Banknote,
} from 'lucide-react'
import {
  useMonthlySummary, useCashFlow, useEntries, useMarkPaidMutation, useDeleteEntryMutation,
} from '../api/queries'
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

function ExpenseTotal() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
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
        <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Total Despesas</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.negative }}>
          {formatBRL(summary?.TotalExpense ?? 0)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Este mês</p>
      </KpiCardContent>
    </KpiCard>
  )
}

function BalanceCard() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const summaryQuery = useMonthlySummary(currentMonth)
  const summary = summaryQuery.data ?? null
  const balance = summary?.ExpectedBalance ?? 0
  const balanceTone = balance >= 0 ? 'positive' : 'negative'

  return (
    <KpiCard
      tone={balanceTone}
      isLoading={summaryQuery.isLoading}
      isError={summaryQuery.isError}
      errorMessage="Erro ao carregar saldo"
      className="min-h-26"
    >
      <KpiCardContent icon={Wallet} tone={balanceTone}>
        <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Saldo Previsto</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar[balanceTone] }}>
          {formatBRL(balance)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Entradas − despesas previstas</p>
      </KpiCardContent>
    </KpiCard>
  )
}

// Faturamento and entradas de caixa are two cards rather than one "Receitas"
// total because they answer different questions and routinely disagree: a loan
// is cash and not a sale, an unpaid crediário sale is a sale and not yet cash.
// One number labelled "Receitas" hid both facts.
function RevenueTotal() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
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
        <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Faturamento</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.positive }}>
          {formatBRL(summary?.TotalRevenue ?? 0)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Vendas deste mês</p>
      </KpiCardContent>
    </KpiCard>
  )
}

function CashInTotal() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
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
        <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Entradas de Caixa</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.info }}>
          {formatBRL(summary?.TotalCashIn ?? 0)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Todo dinheiro recebido</p>
      </KpiCardContent>
    </KpiCard>
  )
}

function Receivables() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')

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
        <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">A Receber</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.info }}>
          {formatBRL(totalReceivable)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Pendente</p>
      </KpiCardContent>
    </KpiCard>
  )
}

export default function Dashboard() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')
  const cashflowQuery = useCashFlow(currentMonth)
  const entriesQuery = useEntries(firstDay, lastDay)
  const markPaid = useMarkPaidMutation()
  const deleteEntry = useDeleteEntryMutation()

  const cashflow = cashflowQuery.data?.points ?? []
  const entries = entriesQuery.data?.entries ?? []

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Painel de Controle</h1>
        <p className="mt-1 text-muted-foreground">Visão geral financeira do estabelecimento</p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <RevenueTotal />
        <CashInTotal />
        <ExpenseTotal />
        <BalanceCard />
        <Receivables />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <ToPayToday />
        <WorstMonth />
        <GoalCard />
      </div>

      <CashFlowChart data={cashflow} />
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
