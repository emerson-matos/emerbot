import { Calendar } from 'lucide-react'
import EmptyState from '@/components/EmptyState'
import { formatBRL } from '@/lib/format'
import { WEEKDAY_FULL_PT, weekdayWithArticle } from '@/lib/weekdays'
import type { TodayTarget, TodayTargetScale, WeekdayStat } from '@/api/types'

// A pharmacy's week has a shape — the weekend carries it, Sunday is a trough —
// and the seven figures used to be printed in seven identical grey boxes, where
// a Saturday of R$ 1.432 and a Sunday of R$ 620 drew exactly the same picture.
// Height is the only encoding a reader gets for free.
//
// The bars are navy: this is what the week *is*. Gold is what is being *asked*
// of it — the dashed mark on today's column is the takings today has to reach
// to keep the month on its goal, and gold appears nowhere else on the page.

function paceTone(status: TodayTargetScale): string {
  switch (status) {
    case 'above':    return 'text-warning'
    case 'below':    return 'text-success'
    case 'on_track': return 'text-muted-foreground'
  }
}

function TodayMark({ target }: { target: TodayTarget }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-t pt-3">
      <span className="text-sm font-medium">Meta para hoje</span>
      <span
        className="text-lg font-semibold tracking-tight tabular-nums"
        style={{ color: 'var(--accent-strong)' }}
      >
        {formatBRL(target.target)}
      </span>
      {/* The sign carries the direction: the sentence was fixed at "acima do
          esperado" and printed "-R$ 500,00 acima do esperado" on a day the
          pharmacy could afford to take lighter. */}
      <span className={`text-sm ${paceTone(target.status)}`}>
        {target.delta === 0
          ? 'Em linha com o esperado'
          : `${formatBRL(Math.abs(target.delta))} ${target.delta > 0 ? 'acima' : 'abaixo'} do esperado`}
      </span>
      <span className="text-sm text-muted-foreground">
        ≈ {target.factor.toLocaleString('pt-BR', { minimumFractionDigits: 1, maximumFractionDigits: 1 })}× o
        esperado para {weekdayWithArticle(target.weekday)}
      </span>
    </div>
  )
}

export default function WeekRhythm({ days, todayTarget }: {
  days: WeekdayStat[]
  todayTarget: TodayTarget
}) {
  // Weekday order, always. Today used to be sorted to the front, which on any
  // day but a Sunday left a strip labelled Qua, Dom, Seg, Ter… — a week that
  // reads as scrambled. Today is marked in place.
  const week = [...days].sort((a, b) => a.day - b.day)
  const traded = week.some((day) => day.count > 0)

  // The scale belongs to the week, never to the target. Letting an extreme ask
  // set it flattened all seven bars into a single line — a goal of R$ 220.000
  // on a pharmacy that takes R$ 1.300 a day pushed the mark to 6,6× the tallest
  // column and destroyed the one thing this strip exists to show. The mark is
  // clamped to the top instead: pinned above every bar, which is exactly what
  // "well beyond an ordinary day" looks like, with the amount and the multiple
  // spelled out underneath.
  const ceiling = Math.max(...week.map((day) => (day.count > 0 ? day.avg : 0))) * 1.15

  const weeksMeasured = Math.max(...week.map((day) => day.count), 0)

  if (!traded) {
    return (
      <>
        <EmptyState
          icon={Calendar}
          message="Nenhuma venda de balcão registrada nas últimas 8 semanas."
          className="py-6"
        />
        {todayTarget.valid && <TodayMark target={todayTarget} />}
      </>
    )
  }

  return (
    <>
      {/* The seven columns never fold: two columns of weekdays is not a week.
          A full "R$ 1.333,26" needs about 66px, more than a seventh of a phone
          gives, so on a narrow screen the strip scrolls sideways instead of
          overlapping its own figures. From `sm` up it fits and never scrolls. */}
      <div className="-mx-1 overflow-x-auto px-1">
        <div className="grid min-w-max grid-cols-7 gap-1 sm:gap-2">
        {week.map((day) => {
          const height = day.count > 0 ? Math.max((day.avg / ceiling) * 100, 6) : 0
          const markAt = day.isToday && todayTarget.valid
            ? Math.min((todayTarget.target / ceiling) * 100, 98)
            : null

          return (
            <div
              key={day.day}
              className="flex min-w-18 flex-col items-center gap-1.5 sm:min-w-0"
              // The sample size is disclosure, not a headline: a weekday
              // measured over three weeks is a weaker average than one measured
              // over eight, but printed under all seven it was a third column
              // of digits nobody was reading.
              title={day.count > 0
                ? `${WEEKDAY_FULL_PT[day.label] ?? day.label}: ${formatBRL(day.avg)} em média (${day.count} ${day.count === 1 ? 'semana' : 'semanas'})`
                : `${WEEKDAY_FULL_PT[day.label] ?? day.label}: sem vendas registradas`}
            >
              {/* Tall and narrow. Stretched across a desktop card the columns
                  came out 170px wide and 100px tall, and a shape that wide is
                  read as a row of blocks rather than as a week. */}
              <div className="relative flex h-32 w-full max-w-14 items-end sm:h-40 sm:max-w-20" aria-hidden>
                {markAt !== null && (
                  <span
                    className="absolute inset-x-0 z-10 border-t-2 border-dashed"
                    style={{ bottom: `${markAt}%`, borderColor: 'var(--accent-strong)' }}
                  />
                )}
                <div
                  className="w-full rounded-t-md"
                  style={{
                    height: `${height}%`,
                    background: day.isToday
                      ? 'var(--primary)'
                      : 'color-mix(in oklch, var(--primary) 40%, transparent)',
                  }}
                />
              </div>
              <p className={`text-xs ${day.isToday ? 'font-semibold' : 'font-medium text-muted-foreground'}`}>
                {day.label}
              </p>
              {/* Sem centavos: a régua é uma média de oito semanas, e os dois
                  dígitos finais dão uma precisão que o número não tem. O valor
                  exato fica no title da coluna. */}
              <p className="text-center text-xs tracking-tight tabular-nums">
                {day.count > 0
                  ? formatBRL(day.avg, { fractionDigits: 0, roundingMode: 'expand' })
                  : '—'}
              </p>
            </div>
          )
        })}
        </div>
      </div>

      <p className="text-xs text-muted-foreground">
        Média de cada dia da semana nas últimas {weeksMeasured} semanas.
        As mais recentes pesam mais (Gaussiana σ=2).
      </p>

      {todayTarget.valid && <TodayMark target={todayTarget} />}
    </>
  )
}
