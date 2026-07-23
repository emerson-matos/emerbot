import type { LucideIcon } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { formatBRL } from '@/lib/format'

export type KpiTone = 'positive' | 'negative' | 'info' | 'warning' | 'neutral'

interface KpiCardProps {
  title: string
  value: number
  icon: LucideIcon
  tone: KpiTone
  subtitle?: string
}

const toneVar: Record<KpiTone, string> = {
  positive: 'var(--success)',
  negative: 'var(--destructive)',
  info: 'var(--info)',
  warning: 'var(--warning)',
  neutral: 'var(--muted-foreground)',
}

export default function KpiCard({ title, value, icon: Icon, tone, subtitle }: KpiCardProps) {
  const c = toneVar[tone]
  return (
    <Card className="relative min-h-26 overflow-hidden">
      {/* accent spine */}
      <span
        aria-hidden
        className="absolute inset-y-0 left-0 w-1"
        style={{ background: c }}
      />
      <CardContent className="flex items-start justify-between gap-3 pl-5">
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            {title}
          </p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: c }}>
            {formatBRL(value)}
          </p>
          {subtitle && <p className="mt-1 text-xs text-muted-foreground">{subtitle}</p>}
        </div>
        <span
          className="grid size-9 shrink-0 place-items-center rounded-lg"
          style={{ background: `color-mix(in oklch, ${c} 14%, transparent)`, color: c }}
        >
          <Icon className="size-[18px]" />
        </span>
      </CardContent>
    </Card>
  )
}
