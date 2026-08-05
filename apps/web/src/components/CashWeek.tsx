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
// with R$ 8.500,00 of bills against R$ 1.480,00 of takings is a fact you act
// on, and a curve dipping gently through it is not.
//
// Each day is two bars side by side on one baseline: what comes in, what goes
// out. They were stacked either side of a zero axis first, which reads as one
// bar per day and makes the eye measure two lengths in opposite directions to
// compare them. Side by side, the comparison is the picture.
//
// Green is money that will land because it is booked; amber is money the
// weekday usually brings and nobody has promised — the same amber the
// three-month chart uses for the month's projection, meaning the same thing:
// this part has not happened yet.

/**
 * One day's pair of bars, both measured against the window's single ceiling.
 *
 * A day that moves almost nothing still gets a sliver, so an empty column is
 * unmistakably empty rather than just short.
 */
function Bars({ day, ceiling }: { day: CashWeekDay; ceiling: number }) {
  const pct = (amount: number) =>
    amount > 0 && ceiling > 0 ? `${Math.max((amount / ceiling) * 100, 3)}%` : '0%'

  return (
    <div className="flex h-32 w-full max-w-16 items-end justify-center gap-1 sm:h-40 sm:max-w-24" aria-hidden>
      {/* Entra. justify-end stacks the expected part above the booked one: the
          uncertain money sits at the top of the bar, furthest from the
          baseline, where it reads as the part that might not arrive. */}
      <div className="flex h-full w-1/2 flex-col justify-end">
        <div
          className="w-full rounded-t-md"
          style={{ height: pct(day.expectedIn), background: 'var(--warning)' }}
        />
        <div
          className={`w-full ${day.expectedIn > 0 ? '' : 'rounded-t-md'}`}
          style={{ height: pct(day.scheduledIn), background: 'var(--success)' }}
        />
      </div>

      {/* Sai. Only what is booked: the backend never invents an unbooked bill,
          because a guessed expense would soften an alarm on evidence nobody
          has. */}
      <div className="flex h-full w-1/2 flex-col justify-end">
        <div
          className="w-full rounded-t-md"
          style={{ height: pct(day.scheduledOut), background: 'var(--destructive)' }}
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
  const { ceiling } = cashWeekPeaks(days)

  if (days.length === 0) {
    return (
      <EmptyState
        icon={Wallet}
        message="Este mês já fechou — não há dias à frente para projetar."
        className="py-6"
      />
    )
  }
  if (ceiling === 0) {
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
              <Bars day={day} ceiling={ceiling} />
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
