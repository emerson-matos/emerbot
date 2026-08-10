import { CalendarClock, Check } from 'lucide-react'
import { formatBRL } from '@/lib/format'
import { todayISO } from '@/lib/entries'
import KpiCard, { KpiCardContent, KpiCardActions, toneVar } from '@/components/KpiCard'
import { Button } from '@/components/ui/button'
import MarkPaidDialog from '@/components/payments/MarkPaidDialog'
import { useEntries, useMarkPaidMutation } from '../api/queries'

export default function ToPayToday() {
  const today = todayISO()

  const entriesQuery = useEntries(today, today)
  const markPaid = useMarkPaidMutation()

  const entries = entriesQuery.data?.entries ?? []
  const pendingToday = entries.filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending')
  const payableToday = pendingToday.reduce((sum, e) => sum + e.Amount, 0)

  // One question for the whole batch: quitting the day's bills one by one is
  // exactly what this button exists to avoid, so asking the form of payment per
  // entry would give the field back the cost the button removed. Somebody who
  // paid one of them differently fixes that one on the edit page.
  const payAllToday = (method: string) => {
    pendingToday.forEach(entry => markPaid.mutate({ entry, method }))
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
        <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">A Pagar Hoje</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.warning }}>
          {formatBRL(payableToday)}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">Vencimentos de hoje</p>
      </KpiCardContent>
      <KpiCardActions>
        {pendingToday.length > 0 && (
          <MarkPaidDialog
            isIncome={false}
            title="Marcar todos como pagos?"
            description={`${pendingToday.length} ${pendingToday.length === 1 ? 'conta' : 'contas'} de hoje, ${formatBRL(payableToday)} no total.`}
            confirmLabel="Confirmar"
            onConfirm={payAllToday}
            trigger={
              <Button
                variant="outline"
                size="sm"
                className="w-full border-warning text-warning hover:bg-warning/10"
                disabled={markPaid.isPending}
              >
                <Check className="size-3.5" /> Pagar
              </Button>
            }
          />
        )}
      </KpiCardActions>
    </KpiCard>
  )
}
