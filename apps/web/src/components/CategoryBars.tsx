import { format } from 'date-fns'
import { PieChart } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '../api/client'
import { categoricalPalette } from '@/lib/chart'
import { categoryLabels } from '@/lib/categories'
import { useCategorySummary } from '../api/queries'
import EmptyState from './EmptyState'

export default function CategoryBars() {
  const now = new Date()
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')

  const categoriesQuery = useCategorySummary(firstDay, lastDay)

  const expenses = (categoriesQuery.data?.categories ?? [])
    .filter(c => c.Type === 'expense')
    .sort((a, b) => b.Total - a.Total)
    .slice(0, 6)

  const maxTotal = expenses[0]?.Total ?? 1

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <PieChart className="size-4 text-primary" aria-hidden />
          Despesas por Categoria
        </CardTitle>
      </CardHeader>
      <CardContent>
        {expenses.length === 0 ? (
          <EmptyState icon={PieChart} message="Sem despesas categorizadas neste período." />
        ) : (
          <div className="space-y-3">
            {expenses.map((cat, i) => (
              <div key={cat.Category}>
                <div className="mb-1 flex justify-between text-xs">
                  <span className="capitalize">{categoryLabels[cat.Category] ?? cat.Category.replace(/_/g, ' ')}</span>
                  <span className="font-medium tabular-nums">{formatBRL(cat.Total)}</span>
                </div>
                <div className="h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full transition-[width] duration-500"
                    style={{
                      width: `${Math.min(100, (cat.Total / maxTotal) * 100)}%`,
                      background: categoricalPalette[i % categoricalPalette.length],
                    }}
                  />
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
