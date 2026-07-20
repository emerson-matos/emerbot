import { format } from 'date-fns'
import {
  Wallet, TrendingUp, TrendingDown, Clock,
} from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useMonthlySummary, useCashFlow, useEntries, useMarkPaidMutation,
} from '../api/queries'
import KpiCard from '../components/KpiCard'
import GoalCard from '../components/GoalCard'
import CashFlowChart from '../components/CashFlowChart'
import IncomeExpenseChart from '../components/IncomeExpenseChart'
import CategoryDonut from '../components/CategoryDonut'
import TransactionsTable from '../components/TransactionsTable'
import WorstMonth from './WorstMonth';
import MonthlyExpent from './MonthlyExpent';
import ToPayToday from './ToPayToday';

function ExpenseTotal() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')

  const summaryQuery = useMonthlySummary(currentMonth)

  const summary = summaryQuery.data ?? null

  if (summaryQuery.isLoading) {
    return <Skeleton className="h-26 rounded-xl" />
  }
  return <KpiCard title="Total Despesas" value={summary?.TotalExpense ?? 0} icon={TrendingDown} tone="negative" subtitle="Este mês" />
}

function BalanceCard() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')

  const summaryQuery = useMonthlySummary(currentMonth)
  const summary = summaryQuery.data ?? null
  const balance = summary?.Balance ?? 0
  if (summaryQuery.isLoading) {
    return <Skeleton className="h-26 rounded-xl" />
  }
  return <KpiCard title="Saldo do Mês" value={balance} icon={Wallet} tone={balance >= 0 ? 'positive' : 'negative'} subtitle="Receitas − Despesas" />
}

function TotalReceivable() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')

  const summaryQuery = useMonthlySummary(currentMonth)

  const summary = summaryQuery.data ?? null

  if (summaryQuery.isLoading) {
    return <Skeleton className="h-26 rounded-xl" />
  }

  return <KpiCard title="Total Receitas" value={summary?.TotalIncome ?? 0} icon={TrendingUp} tone="positive" subtitle="Este mês" />
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


  if (summaryQuery.isLoading) {
    return <Skeleton className="h-26 rounded-xl" />
  }

  return <KpiCard title="A Receber" value={totalReceivable} icon={Clock} tone="info" subtitle="Pendente" />
}

export default function Dashboard() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), now.getDay()), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')
  const cashflowQuery = useCashFlow(currentMonth)
  const entriesQuery = useEntries(firstDay, lastDay)
  const markPaid = useMarkPaidMutation(firstDay, lastDay)

  const cashflow = cashflowQuery.data?.points ?? []
  const entries = entriesQuery.data?.entries ?? []

  return (
    <div className="space-y-6">

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <BalanceCard />
        <TotalReceivable />
        <ExpenseTotal />
        <Receivables />
      </div>

      {/* Secondary strip */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <ToPayToday />
        <WorstMonth />
        <GoalCard />
      </div>

      {/* Charts */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-3"><CashFlowChart data={cashflow} /></div>
      </div>

      {/* Breakdown */}
      <div className="grid grid-cols-1  gap-3 lg:grid-cols-3">
        <IncomeExpenseChart />
        <CategoryDonut />
        <MonthlyExpent />
      </div>

      <TransactionsTable
        entries={entries}
        isLoading={entriesQuery.isLoading}
        onMarkPaid={(id: string) => markPaid.mutate(id)}
      />
    </div>
  )
}


