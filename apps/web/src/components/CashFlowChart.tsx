import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, ReferenceLine,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '../api/client'
import type { CashFlowPoint } from '../api/client'
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

  const refLines = [
    <ReferenceLine key="zero" y={0} stroke="#ef4444" strokeDasharray="4 4" />,
  ]
  if (todayIndex >= 0) {
    refLines.push(
      <ReferenceLine
        key="today"
        x={formatted[todayIndex].label}
        stroke="#3b82f6"
        strokeDasharray="4 4"
        label={{ value: 'hoje', position: 'top', fontSize: 10, fill: '#3b82f6' }}
      />
    )
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">📈 Fluxo de Caixa — 30 dias (passado + projeção)</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={220}>
          <LineChart data={formatted} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
            <XAxis dataKey="label" tick={{ fontSize: 11, fill: '#9ca3af' }} interval={4} />
            <YAxis tick={{ fontSize: 11, fill: '#9ca3af' }} tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`} />
            <Tooltip formatter={value => [formatBRL(Number(value) * 100), 'Saldo']} labelStyle={{ fontSize: 12 }} />
            {refLines}
            <Line type="monotone" dataKey="balance" stroke="#10b981" strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
          </LineChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}
