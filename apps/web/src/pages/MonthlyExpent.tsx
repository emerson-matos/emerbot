import { format, parseISO } from 'date-fns'
import {
  Flame, Info,
} from 'lucide-react'
import { formatBRL } from '@/lib/format'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { categoricalPalette } from '@/lib/chart'
import { categoryLabelMap } from '@/lib/categories'
import {
  useCategories, useCategorySummary, useEntries,
} from '../api/queries'
import EmptyState from '../components/EmptyState'

export default function MonthlyExpent() {
  const now = new Date()
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')

  const categoriesQuery = useCategorySummary(firstDay, lastDay)
  const entriesQuery = useEntries(firstDay, lastDay)
  const categoryDefsQuery = useCategories()

  if (categoriesQuery.isLoading || entriesQuery.isLoading) {
    return <Card className="min-h-52"><CardContent className="flex grow items-center justify-center"><Skeleton className="size-full rounded-xl" /></CardContent></Card>
  }

  if (categoriesQuery.isError || entriesQuery.isError) {
    return <Card className="min-h-52"><CardContent className="flex grow items-center justify-center"><p className="text-xs text-destructive">Erro ao carregar gastos do mês</p></CardContent></Card>
  }

  const categories = categoriesQuery.data?.categories ?? []
  const entries = entriesQuery.data?.entries ?? []
  const labels = categoryLabelMap(categoryDefsQuery.data ?? [])

  const expenseByDay: Record<string, number> = {}
  for (const e of entries) {
    if (e.Type === 'expense') {
      const day = (e.TransactionDate ?? '').slice(0, 10)
      expenseByDay[day] = (expenseByDay[day] ?? 0) + e.Amount
    }
  }

  const worstDayEntry = Object.entries(expenseByDay).sort((a, b) => b[1] - a[1])[0]
  const worstDay = worstDayEntry
    ? { date: format(parseISO(worstDayEntry[0]), 'dd/MM'), total: worstDayEntry[1] }
    : null
  const topExpenses = categories
    .filter(c => c.Type === 'expense')
    .sort((a, b) => b.Total - a.Total)
    .slice(0, 5)

  return (
    <Card className="min-h-52">
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
                  <p className="truncate text-sm font-medium capitalize">{labels[cat.Category] ?? cat.Category.replace(/_/g, ' ')}</p>
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
  )
}
