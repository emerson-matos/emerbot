import { format } from 'date-fns'
import { CalendarClock } from 'lucide-react'
import { formatBRL } from '../api/client'
import { Card, CardContent } from '@/components/ui/card'
import { useEntries } from '../api/queries'

export default function ToPayToday() {
  const today = format(new Date(), 'yyyy-MM-dd')

  // Only ever cares about entries due today, so ask the server for just that
  // one day instead of pulling the whole month and filtering client-side.
  const entriesQuery = useEntries(today, today)

  const entries = entriesQuery.data?.entries ?? []

  const payableToday = entries
    .filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending')
    .reduce((sum, e) => sum + e.Amount, 0)

  return (
    <Card className="relative overflow-hidden">
      <span aria-hidden className="absolute inset-y-0 left-0 w-1" style={{ background: 'var(--warning)' }} />
      <CardContent className="flex items-start justify-between gap-3 pl-5">
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">A Pagar Hoje</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums text-warning">{formatBRL(payableToday)}</p>
          <p className="mt-1 text-xs text-muted-foreground">Vencimentos de hoje</p>
        </div>
        <span className="grid size-9 shrink-0 place-items-center rounded-lg text-warning" style={{ background: 'color-mix(in oklch, var(--warning) 15%, transparent)' }}>
          <CalendarClock className="size-[18px]" />
        </span>
      </CardContent>
    </Card>
  )
}
