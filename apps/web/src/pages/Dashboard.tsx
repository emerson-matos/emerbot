import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import {
  Wallet, TrendingUp, TrendingDown, Clock, CalendarClock,
  Flame, CalendarX, Info,
} from 'lucide-react'
import { formatBRL } from '../api/client'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { categoricalPalette } from '@/lib/chart'
import {
  useMonthlySummary, useCategorySummary, useCashFlow, useEntries,
  useMonthlyTrend, useMarkPaidMutation,
} from '../api/queries'
import AppLayout from '../components/AppLayout'
import KpiCard from '../components/KpiCard'
import GoalCard from '../components/GoalCard'
import CashFlowChart from '../components/CashFlowChart'
import IncomeExpenseChart from '../components/IncomeExpenseChart'
import CategoryDonut from '../components/CategoryDonut'
import TransactionsTable from '../components/TransactionsTable'
import EmptyState from '../components/EmptyState'

export default function Dashboard() {
  const userName = localStorage.getItem('user_name') ?? 'você'
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const monthLabel = format(now, "MMMM 'de' yyyy", { locale: ptBR })
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd')
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

  function handleLogout() {
    localStorage.clear()
    window.location.href = '/login'
  }

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

  const payableToday = entries
    .filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending' &&
      e.DueDate && e.DueDate.startsWith(format(new Date(), 'yyyy-MM-dd')))
    .reduce((sum, e) => sum + e.Amount, 0)

  const totalReceivable = entries
    .filter(e => e.Type === 'income' && e.PaymentStatus === 'pending')
    .reduce((sum, e) => sum + e.Amount, 0)

  const topExpenses = categories
    .filter(c => c.Type === 'expense')
    .sort((a, b) => b.Total - a.Total)
    .slice(0, 5)

  const worstMonth = monthlyData.length === 3
    ? monthlyData.reduce((prev, cur) =>
        prev.income - prev.expense < cur.income - cur.expense ? prev : cur
      )
    : null

  const expenseByDay: Record<string, number> = {}
  for (const e of entries) {
    if (e.Type === 'expense') {
      const day = e.Date.slice(0, 10)
      expenseByDay[day] = (expenseByDay[day] ?? 0) + e.Amount
    }
  }
  const worstDayEntry = Object.entries(expenseByDay).sort((a, b) => b[1] - a[1])[0]
  const worstDay = worstDayEntry
    ? { date: format(new Date(worstDayEntry[0]), 'dd/MM'), total: worstDayEntry[1] }
    : null

  const balance = summary?.Balance ?? 0
  const kpisLoading = summaryQuery.isLoading || entriesQuery.isLoading

  return (
    <AppLayout userName={userName} subtitle={monthLabel} onLogout={handleLogout}>
      <div className="space-y-6">
        {kpisLoading ? (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-[104px] rounded-xl" />
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
          <KpiCard title="A Pagar Hoje" value={payableToday} icon={CalendarClock} tone="warning" subtitle="Vencimento hoje" />
          {worstMonth ? (
            <Card className="relative overflow-hidden">
              <span aria-hidden className="absolute inset-y-0 left-0 w-1 bg-destructive" />
              <CardContent className="flex items-center gap-3 pl-5">
                <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-destructive/15 text-destructive">
                  <CalendarX className="size-[18px]" />
                </span>
                <div className="min-w-0">
                  <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Pior Mês</p>
                  <p className="mt-0.5 text-sm">
                    <strong className="capitalize">{worstMonth.month}</strong>
                    {' — '}
                    <span className="tabular-nums">{formatBRL(worstMonth.income - worstMonth.expense)}</span>
                  </p>
                </div>
              </CardContent>
            </Card>
          ) : <div className="hidden lg:block" />}
          <GoalCard month={currentMonth} summary={summary} />
        </div>

        {/* Charts */}
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <div className="lg:col-span-2"><CashFlowChart data={cashflow} /></div>
          <IncomeExpenseChart data={monthlyData} />
        </div>

        {/* Breakdown */}
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Card>
            <CardContent className="space-y-4">
              <h3 className="flex items-center gap-2 text-sm font-semibold">
                <Flame className="size-4 text-primary" aria-hidden />
                Maiores Gastos do Mês
              </h3>
              {topExpenses.length === 0 ? (
                <EmptyState icon={Flame} message="Sem gastos registrados neste mês." />
              ) : (
                <div className="space-y-3">
                  {topExpenses.map((cat, i) => (
                    <div key={cat.Category} className="flex items-center gap-3">
                      <span className="size-2.5 shrink-0 rounded-full" style={{ background: categoricalPalette[i % categoricalPalette.length] }} />
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium capitalize">{cat.Category.replace(/_/g, ' ')}</p>
                        <p className="text-xs text-muted-foreground">{cat.Count} registro(s)</p>
                      </div>
                      <span className="text-sm font-semibold tabular-nums">{formatBRL(cat.Total)}</span>
                    </div>
                  ))}
                </div>
              )}
              {worstDay && (
                <div className="flex items-center justify-between border-t border-border pt-3">
                  <span className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                    <Info className="size-3.5" /> Pior dia — {worstDay.date}
                  </span>
                  <span className="text-sm font-semibold tabular-nums text-destructive">{formatBRL(worstDay.total)}</span>
                </div>
              )}
            </CardContent>
          </Card>
          <CategoryDonut data={categories} />
        </div>

        <TransactionsTable
          entries={entries}
          isLoading={entriesQuery.isLoading}
          onMarkPaid={(id: string) => markPaid.mutate(id)}
        />
      </div>
    </AppLayout>
  )
}
