import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip,
  Legend, ResponsiveContainer,
} from 'recharts'
import { BarChart3 } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '../api/client'
import { chartColor, tooltipProps } from '@/lib/chart'
import { format } from 'date-fns';
import { useMonthlyTrend } from '@/api/queries';
import { ptBR } from 'date-fns/locale';

export default function IncomeExpenseChart() {
  const now = new Date()
  const months3 = [-2, -1, 0].map(offset =>
    format(new Date(now.getFullYear(), now.getMonth() + offset, 1), 'yyyy-MM'),
  )
  const trendQueries = useMonthlyTrend(months3)
  const data = trendQueries.every(q => q.isSuccess)
    ? trendQueries.map((q, i) => ({
      month: format(new Date(months3[i] + '-01'), 'MMM', { locale: ptBR }),
      income: q.data!.TotalIncome,
      expense: q.data!.TotalExpense,
    }))
    : []


  const chartData = data.map(d => ({
    name: d.month,
    Receitas: d.income / 100,
    Despesas: d.expense / 100,
  }))

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <BarChart3 className="size-4 text-primary" aria-hidden />
          Entradas × Saídas — 3 meses
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={chartData} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={chartColor.grid} vertical={false} />
            <XAxis dataKey="name" tick={{ fontSize: 12, fill: chartColor.axis }} tickLine={false} axisLine={false} />
            <YAxis
              tick={{ fontSize: 11, fill: chartColor.axis }}
              tickLine={false}
              axisLine={false}
              tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
            />
            <Tooltip {...tooltipProps} formatter={v => formatBRL(Number(v) * 100)} />
            <Legend wrapperStyle={{ fontSize: 12 }} iconType="circle" />
            <Bar dataKey="Receitas" fill={chartColor.income} radius={[4, 4, 0, 0]} maxBarSize={44} />
            <Bar dataKey="Despesas" fill={chartColor.expense} radius={[4, 4, 0, 0]} maxBarSize={44} />
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}
