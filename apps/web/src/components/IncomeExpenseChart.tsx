import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip,
  Legend, ResponsiveContainer,
} from 'recharts'
import { BarChart3 } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '../api/client'
import { chartColor, tooltipProps } from '@/lib/chart'

interface MonthlyData {
  month: string
  income: number
  expense: number
}

interface Props {
  data: MonthlyData[]
}

export default function IncomeExpenseChart({ data }: Props) {
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
