import { format } from 'date-fns'
import {
  Wallet, TrendingUp, TrendingDown, Clock,
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
        <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Total Despesas</p>
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
  const balance = summary?.Balance ?? 0
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
        <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Saldo do Mês</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar[balanceTone] }}>
          {formatBRL(balance)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Receitas − Despesas</p>
      </KpiCardContent>
    </KpiCard>
  )
}

function TotalReceivable() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const summaryQuery = useMonthlySummary(currentMonth)
  const summary = summaryQuery.data ?? null

  return (
    <KpiCard
      tone="positive"
      isLoading={summaryQuery.isLoading}
      isError={summaryQuery.isError}
      errorMessage="Erro ao carregar receitas"
      className="min-h-26"
    >
      <KpiCardContent icon={TrendingUp} tone="positive">
        <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Total Receitas</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.positive }}>
          {formatBRL(summary?.TotalIncome ?? 0)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Este mês</p>
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
        <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">A Receber</p>
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
        <BalanceCard />
        <TotalReceivable />
        <ExpenseTotal />
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
        onMarkPaid={(id: string) => markPaid.mutate(id)}
        onDelete={(id: string) => deleteEntry.mutate(id)}
      />
    </div>
  )
}
