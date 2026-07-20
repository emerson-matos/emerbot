import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import {
  Wallet, TrendingUp, TrendingDown, Clock, CalendarClock,
  Check,
} from 'lucide-react'
import { formatBRL } from '../api/client'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useMonthlySummary, useCategorySummary, useCashFlow, useEntries,
  useMonthlyTrend, useMarkPaidMutation,
} from '../api/queries'
import KpiCard from '../components/KpiCard'
import GoalCard from '../components/GoalCard'
import CashFlowChart from '../components/CashFlowChart'
import IncomeExpenseChart from '../components/IncomeExpenseChart'
import CategoryDonut from '../components/CategoryDonut'
import TransactionsTable from '../components/TransactionsTable'
import WorstMonth from './WorstMonth';
import MonthlyExpent from './MonthlyExpent';

export default function Dashboard() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), now.getDay() - 1), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')
  const months3 = [-2, -1, 0].map(offset =>
    format(new Date(now.getFullYear(), now.getMonth() + offset, 1), 'yyyy-MM'),
  )

  const summaryQuery = useMonthlySummary(currentMonth)
  const categoriesQuery = useCategorySummary()
  const cashflowQuery = useCashFlow(currentMonth)
  const entriesQuery = useEntries(firstDay, lastDay)
  const trendQueries = useMonthlyTrend(months3)
  const markPaid = useMarkPaidMutation(firstDay, lastDay)


  const summary = summaryQuery.data ?? null
  const categories = categoriesQuery.data?.categories ?? []
  const cashflow = cashflowQuery.data?.points ?? []
  const entries = entriesQuery.data?.entries ?? []

  const monthlyData = trendQueries.every(q => q.isSuccess)
    ? trendQueries.map((q, i) => ({
      month: format(new Date(months3[i] + '-01'), 'MMM', { locale: ptBR }),
      income: q.data!.TotalIncome,
      expense: q.data!.TotalExpense,
    }))
    : []

  const todaysDueExpenses = entries.filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending' &&
    e.DueDate && e.DueDate.startsWith(format(new Date(), 'yyyy-MM-dd')))
  const payableToday = todaysDueExpenses.reduce((sum, e) => sum + e.Amount, 0)

  const totalReceivable = entries
    .filter(e => e.Type === 'income' && e.PaymentStatus === 'pending')
    .reduce((sum, e) => sum + e.Amount, 0)

  const balance = summary?.Balance ?? 0
  const kpisLoading = summaryQuery.isLoading || entriesQuery.isLoading

  return (
    <div className="space-y-6">
      {kpisLoading ? (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-26 rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <KpiCard title="Saldo do Mês" value={balance} icon={Wallet} tone={balance >= 0 ? 'positive' : 'negative'} subtitle="Receitas − Despesas" />
          <KpiCard title="Total Receitas" value={summary?.TotalIncome ?? 0} icon={TrendingUp} tone="positive" subtitle="Este mês" />
          <KpiCard title="Total Despesas" value={summary?.TotalExpense ?? 0} icon={TrendingDown} tone="negative" subtitle="Este mês" />
          <KpiCard title="A Receber" value={totalReceivable} icon={Clock} tone="info" subtitle="Pendente" />
        </div>
      )}

      {/* Secondary strip */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Card className="relative overflow-hidden">
          <span aria-hidden className="absolute inset-y-0 left-0 w-1" style={{ background: 'var(--warning)' }} />
          <CardContent className="flex flex-col gap-3 pl-5">
            <div className="flex items-center gap-3">
              <span className="grid size-9 shrink-0 place-items-center rounded-lg text-warning" style={{ background: 'color-mix(in oklch, var(--warning) 15%, transparent)' }}>
                <CalendarClock className="size-4.5" />
              </span>
              <div className="min-w-0">
                <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">A Pagar Hoje</p>
                <p className="mt-0.5 text-lg font-semibold tabular-nums">{formatBRL(payableToday)}</p>
                <p className="text-xs text-muted-foreground">Vencimento hoje</p>
              </div>
            </div>
            {payableToday > 0 && (
              <Button
                variant="outline"
                size="sm"
                className="w-full border-warning text-warning hover:bg-warning/10"
                onClick={() => {
                  if (!window.confirm('Marcar todos os pagamentos de hoje como pagos?')) return
                  todaysDueExpenses.forEach(e => markPaid.mutate(e.EntryID))
                }}
              >
                <Check className="size-3.5" /> Pagar
              </Button>
            )}
          </CardContent>
        </Card>
        <WorstMonth />
        <GoalCard month={currentMonth} summary={summary} />
      </div>

      {/* Charts */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-3"><CashFlowChart data={cashflow} /></div>
      </div>

      {/* Breakdown */}
      <div className="grid grid-cols-1  gap-3 lg:grid-cols-3">
        <IncomeExpenseChart data={monthlyData} />
        <CategoryDonut data={categories} />
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
