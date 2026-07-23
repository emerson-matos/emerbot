import { format } from 'date-fns'
import { CalendarClock, Check } from 'lucide-react'
import { formatBRL } from '@/lib/format'
import KpiCard, { KpiCardContent, KpiCardActions, toneVar } from '@/components/KpiCard'
import { Button } from '@/components/ui/button'
import { useEntries, useMarkPaidMutation } from '../api/queries'

export default function ToPayToday() {
  const today = format(new Date(), 'yyyy-MM-dd')

  const entriesQuery = useEntries(today, today)
  const markPaid = useMarkPaidMutation()

  const entries = entriesQuery.data?.entries ?? []
  const pendingToday = entries.filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending')
  const payableToday = pendingToday.reduce((sum, e) => sum + e.Amount, 0)

  const payAllToday = () => {
    if (!window.confirm('Marcar todos os pagamentos de hoje como pagos?')) return
    pendingToday.forEach(e => markPaid.mutate(e.EntryID))
  }

  return (
    <KpiCard
      tone="warning"
      isLoading={entriesQuery.isLoading}
      isError={entriesQuery.isError}
      errorMessage="Erro ao carregar vencimentos de hoje"
      className="min-h-39"
    >
      <KpiCardContent icon={CalendarClock} tone="warning">
        <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">A Pagar Hoje</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.warning }}>
          {formatBRL(payableToday)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Vencimentos de hoje</p>
      </KpiCardContent>
      <KpiCardActions>
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
      </KpiCardActions>
    </KpiCard>
  )
}
