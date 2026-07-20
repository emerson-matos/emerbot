import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import {
  CalendarX,
} from 'lucide-react'
import { formatBRL } from '../api/client'
import { Card, CardContent } from '@/components/ui/card'
import {
  useEntries,
  useMonthlyTrend,
} from '../api/queries'

export default function WorstMonth() {
  const now = new Date()
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')
  const months3 = [-2, -1, 0].map(offset =>
    format(new Date(now.getFullYear(), now.getMonth() + offset, 1), 'yyyy-MM'),
  )

  const entriesQuery = useEntries(firstDay, lastDay)
  const trendQueries = useMonthlyTrend(months3)

  const entries = entriesQuery.data?.entries ?? []

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

  const expenseByDay: Record<string, number> = {}
  for (const e of entries) {
    if (e.Type === 'expense') {
      const day = e.Date.slice(0, 10)
      expenseByDay[day] = (expenseByDay[day] ?? 0) + e.Amount
    }
  }
  return (
    <Card className="relative overflow-hidden" >
      <span aria-hidden className="absolute inset-y-0 left-0 w-1 bg-destructive" />
      <CardContent className="flex items-center gap-3 pl-5">
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-destructive/15 text-destructive">
          <CalendarX className="size-[18px]" />
        </span>
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Pior Mês</p>
          <div className="mt-0.5 text-sm">
            {worstMonth ? (
              <>
                <strong className="capitalize">{worstMonth.month}</strong>
                {' — '}
                <span className="tabular-nums">{formatBRL(worstMonth.income - worstMonth.expense)}</span>
              </>
            ) : (<p> sem dados por enquanto</p>)}
          </div>
        </div>
      </CardContent>
    </Card >
  )
}

