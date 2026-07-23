import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import {
  CalendarX,
} from 'lucide-react'
import { formatBRL } from '@/lib/format'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import ErrorState from '@/components/ErrorState'
import { useMonthlyTrend } from '../api/queries'

export default function WorstMonth() {
  const now = new Date()
  const months3 = [-2, -1, 0].map(offset =>
    format(new Date(now.getFullYear(), now.getMonth() + offset, 1), 'yyyy-MM'),
  )

  const trendQueries = useMonthlyTrend(months3)

  if (trendQueries.some(q => q.isLoading)) {
    return <Card className="min-h-26"><CardContent className="flex grow items-center justify-center"><Skeleton className="size-full rounded-xl" /></CardContent></Card>
  }
  if (trendQueries.some(q => q.isError)) {
    return <Card className="min-h-26"><CardContent className="flex grow items-center justify-center"><ErrorState message="Erro ao carregar meses anteriores" /></CardContent></Card>
  }

  const monthlyData = trendQueries.every(q => q.isSuccess)
    ? trendQueries.map((q, i) => ({
      month: format(new Date(months3[i] + '-01'), 'MMM', { locale: ptBR }),
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
    <Card className="relative min-h-26 overflow-hidden">
      <span aria-hidden className="absolute inset-y-0 left-0 w-1 bg-destructive" />
      <CardContent className="flex items-start justify-between gap-3 pl-5">
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Pior Mês</p>
          {worstMonth ? (
            <>
              <p className="mt-1 text-2xl font-semibold tabular-nums text-destructive">
                {formatBRL(worstMonth.income - worstMonth.expense)}
              </p>
              <p className="mt-1 text-xs capitalize text-muted-foreground">{worstMonth.month}</p>
            </>
          ) : (
            <p className="mt-1 text-xs text-muted-foreground">sem dados por enquanto</p>
          )}
        </div>
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-destructive/15 text-destructive">
          <CalendarX className="size-4.5" />
        </span>
      </CardContent>
    </Card>
  )
}

