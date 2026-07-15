import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, ReferenceLine,
} from 'recharts'
import { LineChart as LineChartIcon } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '../api/client'
import type { CashFlowPoint } from '../api/client'
import { chartColor, tooltipProps } from '@/lib/chart'
import { format, parseISO } from 'date-fns'
import { ptBR } from 'date-fns/locale'

interface Props {
  data: CashFlowPoint[]
}

export default function CashFlowChart({ data }: Props) {
  const today = format(new Date(), 'yyyy-MM-dd')
  let todayIndex = -1

  const formatted = data.map((p, i) => {
    if (p.Date === today) todayIndex = i
    return {
      ...p,
      label: format(parseISO(p.Date), 'dd/MM', { locale: ptBR }),
      balance: p.RunningBalance / 100,
    }
  })

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <LineChartIcon className="size-4 text-primary" aria-hidden />
          Fluxo de Caixa do Mês
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={220}>
          <AreaChart data={formatted} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id="cashflow-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={chartColor.income} stopOpacity={0.28} />
                <stop offset="100%" stopColor={chartColor.income} stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke={chartColor.grid} vertical={false} />
            <XAxis dataKey="label" tick={{ fontSize: 11, fill: chartColor.axis }} interval={4} tickLine={false} axisLine={false} />
            <YAxis
              tick={{ fontSize: 11, fill: chartColor.axis }}
              tickLine={false}
              axisLine={false}
              tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
            />
            <Tooltip {...tooltipProps} formatter={value => [formatBRL(Number(value) * 100), 'Saldo']} />
            <ReferenceLine y={0} stroke={chartColor.expense} strokeDasharray="4 4" />
            {todayIndex >= 0 && (
              <ReferenceLine
                x={formatted[todayIndex].label}
                stroke={chartColor.today}
                strokeDasharray="4 4"
                label={{ value: 'hoje', position: 'top', fontSize: 10, fill: chartColor.today }}
              />
            )}
            <Area
              type="monotone"
              dataKey="balance"
              stroke={chartColor.income}
              strokeWidth={2}
              fill="url(#cashflow-fill)"
              dot={false}
              activeDot={{ r: 4 }}
            />
          </AreaChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}
