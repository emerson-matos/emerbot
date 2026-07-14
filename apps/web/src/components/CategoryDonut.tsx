import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '../api/client'
import type { CategorySummary } from '../api/client'

const COLORS = ['#ef4444', '#f97316', '#f59e0b', '#3b82f6', '#8b5cf6', '#06b6d4', '#10b981', '#ec4899']

interface Props {
  data: CategorySummary[]
}

export default function CategoryDonut({ data }: Props) {
  const expenses = data
    .filter(c => c.Type === 'expense')
    .sort((a, b) => b.Total - a.Total)
    .slice(0, 8)

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">📊 Despesas por Categoria</CardTitle>
      </CardHeader>
      <CardContent>
        {expenses.length === 0 ? (
          <p className="text-muted-foreground text-sm text-center py-8">Sem dados</p>
        ) : (
          <ResponsiveContainer width="100%" height={260}>
            <BarChart
              data={expenses}
              layout="vertical"
              margin={{ top: 0, right: 16, left: 0, bottom: 0 }}
              barSize={20}
              barGap={4}
            >
              <XAxis type="number" tick={{ fontSize: 11, fill: '#9ca3af' }} tickFormatter={v => `R$${(v / 100).toFixed(0)}`} />
              <YAxis
                type="category"
                dataKey="Category"
                tick={{ fontSize: 12, fill: '#6b7280' }}
                width={120}
                tickFormatter={v => v.replace(/_/g, ' ')}
              />
              <Tooltip
                formatter={value => [formatBRL(Number(value)), 'Total']}
                labelFormatter={label => label.replace(/_/g, ' ')}
                contentStyle={{ fontSize: 13 }}
              />
              <Bar dataKey="Total" radius={[0, 4, 4, 0]}>
                {expenses.map((_, i) => (
                  <Cell key={i} fill={COLORS[i % COLORS.length]} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  )
}
