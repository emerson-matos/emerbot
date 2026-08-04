import { CreditCard, CalendarClock, LineChart, Percent } from 'lucide-react'
import {
  usePaymentsSales, usePaymentsReceivables, usePaymentsForecast,
} from '../api/queries'
import KpiCard, { KpiCardContent, toneVar } from './KpiCard'
import { formatBRL } from '@/lib/format'

const labelClass = 'text-xs font-medium tracking-wide text-muted-foreground uppercase'
const valueClass = 'mt-1 text-2xl font-semibold tabular-nums'
const subClass = 'mt-1 text-xs text-muted-foreground'

// The cards take the period as props (rather than reading "this month"
// themselves) so the page's month selector drives all of them from one source.
interface RangeProps {
  firstDay: string
  lastDay: string
  monthLabel: string
}

// SalesCard shows imported card sales for the period (gross) — "how much did I
// sell through the machines this month".
export function SalesCard({ firstDay, lastDay, monthLabel }: RangeProps) {
  const q = usePaymentsSales(firstDay, lastDay)
  const totals = q.data?.totals

  return (
    <KpiCard
      tone="primary"
      isLoading={q.isLoading}
      isError={q.isError}
      errorMessage="Erro ao carregar vendas"
      className="min-h-26"
    >
      <KpiCardContent icon={CreditCard} tone="primary">
        <p className={labelClass}>Vendas no Cartão</p>
        <p className={valueClass} style={{ color: toneVar.primary }}>
          {formatBRL(totals?.gross ?? 0)}
        </p>
        <p className={subClass}>Bruto · {monthLabel}</p>
      </KpiCardContent>
    </KpiCard>
  )
}

// FeesCard isolates the cost of accepting cards: total fees paid, plus what share
// of gross sales that represents — the number that answers "the machines are
// eating how much of my revenue?".
export function FeesCard({ firstDay, lastDay, monthLabel }: RangeProps) {
  const q = usePaymentsSales(firstDay, lastDay)
  const totals = q.data?.totals
  const gross = totals?.gross ?? 0
  const fee = totals?.fee ?? 0
  const pct = gross > 0 ? (fee / gross) * 100 : 0

  return (
    <KpiCard
      tone="warning"
      isLoading={q.isLoading}
      isError={q.isError}
      errorMessage="Erro ao carregar taxas"
      className="min-h-26"
    >
      <KpiCardContent icon={Percent} tone="warning">
        <p className={labelClass}>Taxa Paga</p>
        <p className={valueClass} style={{ color: toneVar.warning }}>
          {formatBRL(fee)}
        </p>
        <p className={subClass}>
          {gross > 0
            ? `${pct.toFixed(1).replace('.', ',')}% das vendas · ${monthLabel}`
            : `Sem vendas · ${monthLabel}`}
        </p>
      </KpiCardContent>
    </KpiCard>
  )
}

// ReceivablesCard shows the total expected to arrive from card sales in the period.
export function ReceivablesCard({ firstDay, lastDay, monthLabel }: RangeProps) {
  const q = usePaymentsReceivables(firstDay, lastDay)

  return (
    <KpiCard
      tone="info"
      isLoading={q.isLoading}
      isError={q.isError}
      errorMessage="Erro ao carregar recebíveis"
      className="min-h-26"
    >
      <KpiCardContent icon={CalendarClock} tone="info">
        <p className={labelClass}>A Receber (Cartão)</p>
        <p className={valueClass} style={{ color: toneVar.info }}>
          {formatBRL(q.data?.total ?? 0)}
        </p>
        <p className={subClass}>Recebíveis previstos · {monthLabel}</p>
      </KpiCardContent>
    </KpiCard>
  )
}

// ForecastCard shows the projected end-of-month balance (pharmacy balance +
// receivables − expenses) and flags if cash goes negative during the month.
export function ForecastCard({ month, monthLabel }: { month: string; monthLabel: string }) {
  const q = usePaymentsForecast(month)
  const points = q.data?.points ?? []
  const endBalance = points.length ? points[points.length - 1].running_balance : 0
  const goesNegative = points.some((p) => p.running_balance < 0)
  const tone = endBalance >= 0 ? 'positive' : 'negative'

  return (
    <KpiCard
      tone={tone}
      isLoading={q.isLoading}
      isError={q.isError}
      errorMessage="Erro ao carregar projeção"
      className="min-h-26"
    >
      <KpiCardContent icon={LineChart} tone={tone}>
        <p className={labelClass}>Saldo Projetado</p>
        <p className={valueClass} style={{ color: toneVar[tone] }}>
          {formatBRL(endBalance)}
        </p>
        <p className={subClass}>
          {goesNegative ? `Fica negativo · ${monthLabel}` : `Fim de ${monthLabel}`}
        </p>
      </KpiCardContent>
    </KpiCard>
  )
}
