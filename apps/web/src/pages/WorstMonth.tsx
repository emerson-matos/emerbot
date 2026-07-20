import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import {
  CalendarX,
} from 'lucide-react'
import { formatBRL } from '../api/client'
import { Card, CardContent } from '@/components/ui/card'
import { useMonthlyTrend } from '../api/queries'

export default function WorstMonth() {
  const now = new Date()
  const months3 = [-2, -1, 0].map(offset =>
    format(new Date(now.getFullYear(), now.getMonth() + offset, 1), 'yyyy-MM'),
  )

  const trendQueries = useMonthlyTrend(months3)

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
    <Card className="relative overflow-hidden">
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
          <CalendarX className="size-[18px]" />
        </span>
      </CardContent>
    </Card>
  )
}

