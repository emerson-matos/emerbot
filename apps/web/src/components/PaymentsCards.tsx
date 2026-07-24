import { format } from 'date-fns'
import { CreditCard, CalendarClock, LineChart } from 'lucide-react'
import {
  usePaymentsSales, usePaymentsReceivables, usePaymentsForecast,
} from '../api/queries'
import KpiCard, { KpiCardContent, toneVar } from './KpiCard'
import { formatBRL } from '@/lib/format'

function currentMonthRange() {
  const now = new Date()
  return {
    month: format(now, 'yyyy-MM'),
    firstDay: format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd'),
    lastDay: format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd'),
  }
}

const labelClass = 'text-[11px] font-medium tracking-wide text-muted-foreground uppercase'
const valueClass = 'mt-1 text-2xl font-semibold tabular-nums'
const subClass = 'mt-1 text-xs text-muted-foreground'

// SalesCard shows imported card sales for the current month (gross) and the fees
// they cost — answering "how much did I sell / how much did selling cost me".
export function SalesCard() {
  const { firstDay, lastDay } = currentMonthRange()
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
        <p className={subClass}>Taxas: {formatBRL(totals?.fee ?? 0)} · este mês</p>
      </KpiCardContent>
    </KpiCard>
  )
}

// ReceivablesCard shows the total expected to arrive from card sales this month.
export function ReceivablesCard() {
  const { firstDay, lastDay } = currentMonthRange()
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
        <p className={subClass}>Recebíveis previstos · este mês</p>
      </KpiCardContent>
    </KpiCard>
  )
}

// ForecastCard shows the projected end-of-month balance (pharmacy balance +
// receivables − expenses) and flags if cash goes negative during the month.
export function ForecastCard() {
  const { month } = currentMonthRange()
  const q = usePaymentsForecast(month)
  const points = q.data?.points ?? []
  const endBalance = points.length ? points[points.length - 1].RunningBalance : 0
  const goesNegative = points.some((p) => p.RunningBalance < 0)
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
          {goesNegative ? 'Fica negativo no mês' : 'Projeção fim do mês'}
        </p>
      </KpiCardContent>
    </KpiCard>
  )
}
