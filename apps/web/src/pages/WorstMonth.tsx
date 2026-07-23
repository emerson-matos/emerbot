import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { CalendarX } from 'lucide-react'
import { formatBRL } from '@/lib/format'
import KpiCard, { KpiCardContent } from '@/components/KpiCard'
import { useMonthlyTrend } from '../api/queries'

export default function WorstMonth() {
  const now = new Date()
  const months3 = [-2, -1, 0].map(offset =>
    format(new Date(now.getFullYear(), now.getMonth() + offset, 1), 'yyyy-MM'),
  )

  const trendQueries = useMonthlyTrend(months3)
  const isLoading = trendQueries.some(q => q.isLoading)
  const isError = trendQueries.some(q => q.isError)

  const monthlyData = trendQueries.every(q => q.isSuccess)
    ? trendQueries.map((q, i) => ({
      month: format(new Date(months3[i] + '-01'), 'MMMM', { locale: ptBR }),
      income: q.data!.TotalIncome,
      expense: q.data!.TotalExpense,
    }))
    : []

  const worstMonth = monthlyData.length === 3
    ? monthlyData.reduce((prev, cur) =>
      prev.income - prev.expense < cur.income - cur.expense ? prev : cur
    )
    : null

  return (
    <KpiCard
      tone="negative"
      isLoading={isLoading}
      isError={isError}
      errorMessage="Erro ao carregar meses anteriores"
      className="min-h-26"
    >
      <KpiCardContent icon={CalendarX} tone="negative">
        <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Pior Mês</p>
        {worstMonth ? (
          <>
            <p className="mt-1 text-2xl font-semibold text-destructive tabular-nums">
              {formatBRL(worstMonth.income - worstMonth.expense)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground capitalize">{worstMonth.month}</p>
          </>
        ) : (
          <p className="mt-1 text-xs text-muted-foreground">sem dados por enquanto</p>
        )}
      </KpiCardContent>
    </KpiCard>
  )
}
