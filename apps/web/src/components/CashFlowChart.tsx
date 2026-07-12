import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, ReferenceLine,
} from 'recharts'
import { formatBRL } from '../api/client'
import type { CashFlowPoint } from '../api/client'
import { format, parseISO } from 'date-fns'
import { ptBR } from 'date-fns/locale'

interface Props {
  data: CashFlowPoint[]
}

export default function CashFlowChart({ data }: Props) {
  const formatted = data.map(p => ({
    ...p,
    label: format(parseISO(p.Date), 'dd/MM', { locale: ptBR }),
    balance: p.RunningBalance / 100,
  }))

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-5">
      <h3 className="text-sm font-semibold text-gray-700 mb-4">📈 Fluxo de Caixa — Próximos 30 dias</h3>
      <ResponsiveContainer width="100%" height={220}>
        <LineChart data={formatted} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
          <XAxis
            dataKey="label"
            tick={{ fontSize: 11, fill: '#9ca3af' }}
            interval={4}
          />
          <YAxis
            tick={{ fontSize: 11, fill: '#9ca3af' }}
            tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
          />
          <Tooltip
            formatter={(value: number) => [formatBRL(value * 100), 'Saldo Previsto']}
            labelStyle={{ fontSize: 12 }}
          />
          <ReferenceLine y={0} stroke="#ef4444" strokeDasharray="4 4" />
          <Line
            type="monotone"
            dataKey="balance"
            stroke="#10b981"
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 4 }}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
