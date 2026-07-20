import { format } from 'date-fns'
import {
  CalendarClock,
  Check,
} from 'lucide-react'
import { formatBRL } from '../api/client'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import {
  useEntries,
  useMarkPaidMutation,
} from '../api/queries'

export default function ToPayToday() {
  const today = format(new Date(), 'yyyy-MM-dd')

  // Only ever cares about entries due today, so ask the server for just that
  // one day instead of pulling the whole month and filtering client-side.
  const entriesQuery = useEntries(today, today)
  const markPaid = useMarkPaidMutation(today, today)

  const entries = entriesQuery.data?.entries ?? []

  const todaysDueExpenses = entries.filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending')
  const payableToday = todaysDueExpenses.reduce((sum, e) => sum + e.Amount, 0)


  return (
    <Card className="relative overflow-hidden">
      <span aria-hidden className="absolute inset-y-0 left-0 w-1" style={{ background: 'var(--warning)' }} />
      <CardContent className="flex flex-col gap-3 pl-5">
        <div className="flex items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-lg text-warning" style={{ background: 'color-mix(in oklch, var(--warning) 15%, transparent)' }}>
            <CalendarClock className="size-4.5" />
          </span>
          <div className="min-w-0">
            <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">A Pagar Hoje</p>
            <p className="mt-0.5 text-lg font-semibold tabular-nums">{formatBRL(payableToday)}</p>
            <p className="text-xs text-muted-foreground">Vencimento hoje</p>
          </div>
        </div>
        {payableToday > 0 && (
          <Button
            variant="outline"
            size="sm"
            className="w-full border-warning text-warning hover:bg-warning/10"
            onClick={() => {
              if (!window.confirm('Marcar todos os pagamentos de hoje como pagos?')) return
              todaysDueExpenses.forEach(e => markPaid.mutate(e.EntryID))
            }}
          >
            <Check className="size-3.5" /> Pagar
          </Button>
        )}
      </CardContent>
    </Card>
  )
}
