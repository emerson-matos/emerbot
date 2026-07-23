import { format } from 'date-fns'
import { CalendarClock, Check } from 'lucide-react'
import { formatBRL } from '@/lib/format'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import ErrorState from '@/components/ErrorState'
import { Button } from '@/components/ui/button'
import { useEntries, useMarkPaidMutation } from '../api/queries'

export default function ToPayToday() {
  const today = format(new Date(), 'yyyy-MM-dd')

  // Only ever cares about entries due today, so ask the server for just that
  // one day instead of pulling the whole month and filtering client-side.
  const entriesQuery = useEntries(today, today)
  const markPaid = useMarkPaidMutation()

  if (entriesQuery.isLoading) {
    return <Card className="min-h-52"><CardContent className="flex grow items-center justify-center"><Skeleton className="size-full rounded-xl" /></CardContent><div className="flex min-h-9 items-center px-(--card-spacing) pl-5" /></Card>
  }
  if (entriesQuery.isError) {
    return <Card className="min-h-52"><CardContent className="flex grow items-center justify-center"><ErrorState message="Erro ao carregar vencimentos de hoje" /></CardContent><div className="flex min-h-9 items-center px-(--card-spacing) pl-5" /></Card>
  }

  const entries = entriesQuery.data?.entries ?? []

  const pendingToday = entries.filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending')
  const payableToday = pendingToday.reduce((sum, e) => sum + e.Amount, 0)

  const payAllToday = () => {
    if (!window.confirm('Marcar todos os pagamentos de hoje como pagos?')) return
    pendingToday.forEach(e => markPaid.mutate(e.EntryID))
  }

  return (
    <Card className="relative min-h-52 overflow-hidden">
      <span aria-hidden className="absolute inset-y-0 left-0 w-1" style={{ background: 'var(--warning)' }} />
      <CardContent className="flex grow items-start justify-between gap-3 pl-5">
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">A Pagar Hoje</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums text-warning">{formatBRL(payableToday)}</p>
          <p className="mt-1 text-xs text-muted-foreground">Vencimentos de hoje</p>
        </div>
        <span className="grid size-9 shrink-0 place-items-center rounded-lg text-warning" style={{ background: 'color-mix(in oklch, var(--warning) 15%, transparent)' }}>
          <CalendarClock className="size-4.5" />
        </span>
      </CardContent>
      <div className="flex min-h-9 items-center px-(--card-spacing) pl-5">
        {pendingToday.length > 0 && (
          <Button
            variant="outline"
            size="sm"
            className="w-full border-warning text-warning hover:bg-warning/10 hover:text-warning"
            disabled={markPaid.isPending}
            onClick={payAllToday}
          >
            <Check className="size-3.5" /> Pagar
          </Button>
        )}
      </div>
    </Card>
  )
}
