import { useCategorySummary } from '../api/queries'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import { PieChart } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '../api/client'
import { categoricalPalette, chartColor, tooltipProps } from '@/lib/chart'
import EmptyState from './EmptyState'


export default function CategoryDonut() {
  const categoriesQuery = useCategorySummary()

  const data = categoriesQuery.data?.categories ?? []

  const expenses = data
    .filter(c => c.Type === 'expense')
    .sort((a, b) => b.Total - a.Total)
    .slice(0, 8)

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
          <ResponsiveContainer width="100%" height={260}>
            <BarChart
              data={expenses}
              layout="vertical"
              margin={{ top: 0, right: 16, left: 0, bottom: 0 }}
              barSize={20}
              barGap={4}
            >
              <XAxis
                type="number"
                tick={{ fontSize: 11, fill: chartColor.axis }}
                tickLine={false}
                axisLine={false}
                tickFormatter={v => `R$${(v / 100).toFixed(0)}`}
              />
              <YAxis
                type="category"
                dataKey="Category"
                tick={{ fontSize: 12, fill: chartColor.axis }}
                tickLine={false}
                axisLine={false}
                width={120}
                tickFormatter={v => v.replace(/_/g, ' ')}
              />
              <Tooltip
                {...tooltipProps}
                formatter={value => [formatBRL(Number(value)), 'Total']}
                labelFormatter={label => String(label).replace(/_/g, ' ')}
              />
              <Bar dataKey="Total" radius={[0, 4, 4, 0]}>
                {expenses.map((_, i) => (
                  <Cell key={i} fill={categoricalPalette[i % categoricalPalette.length]} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  )
}
