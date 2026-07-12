import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer, Legend } from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '../api/client'
import type { CategorySummary } from '../api/client'

const COLORS = [
  '#10b981', '#3b82f6', '#f59e0b', '#ef4444',
  '#8b5cf6', '#ec4899', '#06b6d4', '#84cc16',
  '#f97316', '#6366f1',
]

interface Props {
  data: CategorySummary[]
}

export default function CategoryDonut({ data }: Props) {
  const expenses = data.filter(c => c.Type === 'expense').slice(0, 8)

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">🥧 Despesas por Categoria</CardTitle>
      </CardHeader>
      <CardContent>
        {expenses.length === 0 ? (
          <p className="text-muted-foreground text-sm text-center py-8">Sem dados</p>
        ) : (
          <ResponsiveContainer width="100%" height={240}>
            <PieChart>
              <Pie data={expenses} dataKey="Total" nameKey="Category" cx="50%" cy="50%" innerRadius={55} outerRadius={90} paddingAngle={2}>
                {expenses.map((_, i) => (
                  <Cell key={i} fill={COLORS[i % COLORS.length]} />
                ))}
              </Pie>
              <Tooltip formatter={(v: number) => formatBRL(v)} />
              <Legend formatter={(value) => <span className="text-xs text-muted-foreground">{value}</span>} />
            </PieChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  )
}
