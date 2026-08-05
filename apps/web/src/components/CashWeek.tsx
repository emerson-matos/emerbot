import { Wallet } from 'lucide-react'
import { format, parseISO } from 'date-fns'
import EmptyState from '@/components/EmptyState'
import { formatBRL } from '@/lib/format'
import { weekdayFull, weekdayShort } from '@/lib/weekdays'
import { cashWeekDays, cashWeekPeaks, firstNegative } from '@/lib/cashWeek'
import type { CashWeekDay } from '@/lib/cashWeek'
import type { CashPosition } from '@/api/types'

// The week ahead, in money moving rather than in balance. The line chart on the
// dashboard draws where the balance *goes*; this draws what pushes it — a day
// with R$ 4.000 of bills and R$ 900 of takings is a fact you act on, and a
// curve dipping gently through it is not.
//
// Inflow grows up from the axis, outflow down, on one shared scale so the two
// halves are comparable at a glance. Green is money that will land because it
// is booked; amber is money the weekday usually brings and nobody has promised
// — the same amber the three-month chart uses for the month's projection, and
// it means the same thing there: this part has not happened yet.

/**
 * Each half is drawn against its own peak, and the card says so out loud in the
 * caption underneath.
 *
 * One shared scale was tried first and does not survive real data. A pharmacy
 * takes on the order of R$ 1.200,00 a day and pays bills of R$ 8.500,00 — the
 * payroll, a supplier — so a shared linear scale squashes every inflow into a
 * four-pixel sliver and the green/amber split, which is half of what this card
 * is for, disappears. That ratio is the normal week here, not the outlier.
 *
 * The honest cost is that a green bar and a red bar of the same length are not
 * the same amount, so the two things that defuse it travel with the bars: every
 * column prints its own figures, and the caption names both peaks. What the
 * card still shows truthfully is the shape *within* each half — which day of
 * the week carries the takings, and which day the money leaves on.
 */
function Bars({ day, maxIn, maxOut }: { day: CashWeekDay; maxIn: number; maxOut: number }) {
  // A day that moves almost nothing still gets a sliver, so an empty column is
  // unmistakably empty rather than just short.
  const pct = (amount: number, max: number) =>
    amount > 0 && max > 0 ? `${Math.max((amount / max) * 100, 5)}%` : '0%'

  return (
    <div className="w-full max-w-14 sm:max-w-20" aria-hidden>
      {/* Inflow, growing up to the axis. justify-end puts the expected part on
          top of the booked one: the uncertain half sits furthest from the axis,
          where it reads as the part that might not arrive. */}
      <div className="flex h-20 flex-col justify-end sm:h-24">
        <div
          className="w-full rounded-t-md"
          style={{ height: pct(day.expectedIn, maxIn), background: 'var(--warning)' }}
        />
        <div
          className={`w-full ${day.expectedIn > 0 ? '' : 'rounded-t-md'}`}
          style={{ height: pct(day.scheduledIn, maxIn), background: 'var(--success)' }}
        />
      </div>

      {/* The zero line, and it has to be visible: without it the green above and
          the red below touch, and seven columns read as one stacked bar rather
          than as money going two directions. */}
      <div className="my-1 h-px w-full bg-muted-foreground/60" />

      {/* Outflow, hanging from the axis. Only what is booked: the backend never
          invents an unbooked bill, because a guessed expense would soften an
          alarm on evidence nobody has. */}
      <div className="flex h-20 flex-col sm:h-24">
        <div
          className="w-full rounded-b-md"
          style={{ height: pct(day.scheduledOut, maxOut), background: 'var(--destructive)' }}
        />
      </div>
    </div>
  )
}

export default function CashWeek({ position, today }: {
  position: CashPosition
  today: string
}) {
  const days = cashWeekDays(position.forecast, today)
  const { maxIn, maxOut } = cashWeekPeaks(days)

  if (days.length === 0) {
    return (
      <EmptyState
        icon={Wallet}
        message="Este mês já fechou — não há dias à frente para projetar."
        className="py-6"
      />
    )
  }
  if (maxIn + maxOut === 0) {
    return (
      <EmptyState
        icon={Wallet}
        message="Nada entra nem vence nos próximos dias."
        className="py-6"
      />
    )
  }

  const last = days[days.length - 1]
  const negative = firstNegative(days)

  return (
    <>
      {/* Same rule as the weekday strip: seven columns never fold, because two
          rows of days is not a week. On a narrow screen it scrolls sideways. */}
      <div className="-mx-1 overflow-x-auto px-1">
        <div className="grid min-w-max grid-cols-7 gap-1 sm:gap-2">
          {days.map((day) => (
            <div
              key={day.date}
              className="flex min-w-18 flex-col items-center gap-1.5 sm:min-w-0"
              // The exact figures live here rather than under every column: three
              // rows of digits across seven days is a table nobody reads.
              title={[
                `${weekdayFull(parseISO(day.date).getDay())} ${format(parseISO(day.date), 'dd/MM')}`,
                `Entra: ${formatBRL(day.totalIn)}${day.expectedIn > 0 ? ` (${formatBRL(day.scheduledIn)} lançado + ${formatBRL(day.expectedIn)} esperado)` : ''}`,
                `Sai: ${formatBRL(day.scheduledOut)}`,
                `Saldo no fim do dia: ${formatBRL(day.balance)}`,
              ].join('\n')}
            >
              <Bars day={day} maxIn={maxIn} maxOut={maxOut} />
              <p className={`text-xs ${day.isToday ? 'font-semibold' : 'font-medium text-muted-foreground'}`}>
                {day.isToday ? 'Hoje' : weekdayShort(parseISO(day.date).getDay())}
              </p>
              {/* Sem centavos: a metade âmbar é uma média de oito semanas, e os
                  dois dígitos finais dariam a ela uma precisão que não tem. */}
              <p className="text-center text-xs tracking-tight text-success tabular-nums">
                {day.totalIn > 0 ? formatBRL(day.totalIn, { fractionDigits: 0, roundingMode: 'expand' }) : '—'}
              </p>
              <p className="text-center text-xs tracking-tight text-destructive tabular-nums">
                {day.scheduledOut > 0 ? formatBRL(day.scheduledOut, { fractionDigits: 0, roundingMode: 'expand' }) : '—'}
              </p>
            </div>
          ))}
        </div>
      </div>

      <p className="text-xs text-muted-foreground">
        Verde é o que já está lançado para entrar; âmbar é o recebimento médio
        daquele dia da semana, que ainda não é promessa de ninguém. A saída é só
        o que já está lançado — contas não lançadas não são estimadas.
      </p>
      {/* The two halves are drawn against different peaks, so the reader is
          told what each side's tallest bar is worth. Without this, a red bar
          the size of a green one would read as the same amount of money. */}
      <p className="text-xs text-muted-foreground">
        Cada metade tem escala própria: a barra cheia vale{' '}
        {formatBRL(maxIn, { fractionDigits: 0, roundingMode: 'expand' })} em cima e{' '}
        {formatBRL(maxOut, { fractionDigits: 0, roundingMode: 'expand' })} embaixo.
      </p>

      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-t pt-3">
        <span className="text-sm font-medium">
          Saldo em {format(parseISO(last.date), 'dd/MM')}
        </span>
        <span
          className={`text-lg font-semibold tracking-tight tabular-nums ${last.balance >= 0 ? 'text-success' : 'text-destructive'}`}
        >
          {formatBRL(last.balance)}
        </span>
        {/* The week's own trough, named. cashPosition.daysUntilNegative answers
            this for the whole month; a card about seven days has to say whether
            the dip is inside them. */}
        {negative && (
          <span className="text-sm text-destructive">
            Fica negativo em {format(parseISO(negative.date), 'dd/MM')}
          </span>
        )}
      </div>
    </>
  )
}
