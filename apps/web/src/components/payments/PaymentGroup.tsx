import StatusDot from './StatusDot'
import PaymentCard from './PaymentCard'
import { cn } from '@/lib/utils'
import { netAmount } from '@/lib/entries'
import { formatSignedBRL } from '@/lib/format'
import { toneTextClass, type Tone } from '@/lib/tone'
import type { Entry } from '@/api/types'

export interface PaymentGroupData {
  key: string
  label: string
  kind: 'status' | 'period'
  tone: Tone
  items: Entry[]
}

interface Props {
  group: PaymentGroupData
  onMarkPaid?: (id: string) => void
  onDelete?: (id: string) => void
}

export default function PaymentGroup({ group, onMarkPaid, onDelete }: Props) {
  const net = netAmount(group.items)
  const netTone: Tone = net >= 0 ? 'positive' : 'negative'
  const count = group.items.length
  const countLabel = count === 1 ? '1 lançamento' : `${count} lançamentos`

  return (
    <div>
      <div className="flex items-center justify-between gap-3 px-4 py-2 sm:px-6">
        <div className="flex min-w-0 items-center gap-2">
          {group.kind === 'status' && <StatusDot tone={group.tone} />}
          <span
            className={cn(
              'truncate',
              group.kind === 'status'
                ? 'text-xs font-semibold tracking-wide uppercase'
                : 'text-sm font-medium capitalize',
            )}
          >
            {group.label}
          </span>
          <span className="shrink-0 text-xs text-muted-foreground">{countLabel}</span>
        </div>
        <span className={cn('shrink-0 text-sm font-semibold tabular-nums', toneTextClass[netTone])}>
          {formatSignedBRL(net)}
        </span>
      </div>
      <div className="divide-y divide-border px-4 sm:px-6">
        {group.items.map(entry => (
          <PaymentCard key={entry.EntryID} entry={entry} onMarkPaid={onMarkPaid} onDelete={onDelete} />
        ))}
      </div>
    </div>
  )
}
