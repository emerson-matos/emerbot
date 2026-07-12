import { formatBRL } from '../api/client'

interface KpiCardProps {
  title: string
  value: number
  icon: string
  color: 'green' | 'red' | 'blue' | 'yellow'
  subtitle?: string
}

const colorMap = {
  green: 'bg-emerald-50 text-emerald-700 border-emerald-200',
  red: 'bg-red-50 text-red-700 border-red-200',
  blue: 'bg-blue-50 text-blue-700 border-blue-200',
  yellow: 'bg-amber-50 text-amber-700 border-amber-200',
}

export default function KpiCard({ title, value, icon, color, subtitle }: KpiCardProps) {
  return (
    <div className={`rounded-xl border p-5 ${colorMap[color]}`}>
      <div className="flex items-start justify-between">
        <div>
          <p className="text-xs font-medium uppercase tracking-wide opacity-70">{title}</p>
          <p className="text-2xl font-bold mt-1">{formatBRL(value)}</p>
          {subtitle && <p className="text-xs mt-1 opacity-60">{subtitle}</p>}
        </div>
        <span className="text-2xl">{icon}</span>
      </div>
    </div>
  )
}
