import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip,
  Legend, ResponsiveContainer,
} from 'recharts'
import { formatBRL } from '../api/client'

interface Props {
  income: number
  expense: number
  month: string
}

export default function IncomeExpenseChart({ income, expense, month }: Props) {
  const data = [
    {
      name: month,
      Receitas: income / 100,
      Despesas: expense / 100,
    },
  ]

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-5">
      <h3 className="text-sm font-semibold text-gray-700 mb-4">📊 Entradas × Saídas</h3>
      <ResponsiveContainer width="100%" height={200}>
        <BarChart data={data} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
          <XAxis dataKey="name" tick={{ fontSize: 12 }} />
          <YAxis tick={{ fontSize: 11 }} tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`} />
          <Tooltip formatter={(v: number) => formatBRL(v * 100)} />
          <Legend />
          <Bar dataKey="Receitas" fill="#10b981" radius={[4, 4, 0, 0]} />
          <Bar dataKey="Despesas" fill="#ef4444" radius={[4, 4, 0, 0]} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
