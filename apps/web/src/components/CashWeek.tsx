import {
  Bar, CartesianGrid, Cell, ComposedChart, Legend, Line, ReferenceLine,
  ResponsiveContainer, Tooltip, XAxis, YAxis,
} from 'recharts'
import { Wallet } from 'lucide-react'
import { format, parseISO } from 'date-fns'
import EmptyState from '@/components/EmptyState'
import { formatBRL } from '@/lib/format'
import { brlAxisTick, chartColor, tooltipProps } from '@/lib/chart'
import {
  CASH_WEEK_AHEAD, CASH_WEEK_AHEAD_NARROW, CASH_WEEK_BACK,
  cashWeekDays, cashWeekPeaks, cashWeekSeries, firstNegative,
} from '@/lib/cashWeek'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import type { CashPosition } from '@/api/types'

// The week around today, in money moving rather than in balance. The line chart
// on the dashboard draws where the balance *goes*; this draws what pushes it —
// a day with R$ 8.500,00 of bills against R$ 1.480,00 of takings is a fact you
// act on, and a curve dipping gently through it is not.
//
// Each day is two bars side by side: what comes in, what goes out. They were
// stacked either side of a zero axis first, which reads as one bar per day and
// makes the eye measure two lengths in opposite directions to compare them.
// Side by side on one scale, the comparison is the picture — and one scale is
// not optional there, since two bars sharing a baseline are read as comparable
// whether or not they are.
//
// Green is money already booked to land; amber is what the weekday usually
// brings and nobody has promised — the same amber the three-month chart uses
// for the month's projection, meaning the same thing: this has not happened
// yet. Yesterday therefore carries no amber at all, which is what makes it the
// anchor: one real column to judge six estimated ones against.
//
// Over the bars runs the balance at the end of each day, on its own axis to the
// right. The bars answer "what moves"; only the line answers the question
// somebody actually opens this card with — "vou ficar sem dinheiro nesta
// semana?". R$ 8.500,00 leaving on Saturday is a crisis or a Tuesday depending
// entirely on what is in the account, and the zero line is where the difference
// becomes visible.

/** Bolds today, so the column you are standing on is findable at a glance. */
function DayTick({ x, y, payload, days }: {
  x?: number
  y?: number
  payload?: { value?: string; index?: number }
  days: ReturnType<typeof cashWeekSeries>
}) {
  const day = days[payload?.index ?? -1]
  return (
    <text
      x={x} y={y} dy={12}
      textAnchor="middle"
      fontSize={12}
      fontWeight={day?.isToday ? 600 : 400}
      fill={day?.isToday ? 'var(--foreground)' : chartColor.axis}
    >
      {payload?.value}
    </text>
  )
}

export default function CashWeek({ position, today }: {
  position: CashPosition
  today: string
}) {
  // Tailwind's `sm`. Below it the card gets three columns instead of seven —
  // see CASH_WEEK_AHEAD_NARROW. A media query and not a CSS class because what
  // changes is the series, not its styling: seven columns squeezed into a phone
  // are unreadable whatever is done to them.
  const wide = useMediaQuery('(min-width: 40rem)')
  const days = cashWeekDays(
    position.forecast, today, CASH_WEEK_BACK, wide ? CASH_WEEK_AHEAD : CASH_WEEK_AHEAD_NARROW,
  )
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
        message="Nada entra nem vence nesses dias."
        className="py-6"
      />
    )
  }

  const series = cashWeekSeries(days)
  const last = days[days.length - 1]
  const negative = firstNegative(days)
  // The boundary between what happened and what is estimated, drawn where the
  // amber starts. Placed on today's own column rather than between two of them:
  // today is where the estimating begins, since the day is only part traded.
  const todayLabel = series.find((d) => d.isToday)?.label

  return (
    <>
      <ResponsiveContainer width="100%" height={240}>
        <ComposedChart data={series} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={chartColor.grid} vertical={false} />
          <XAxis
            dataKey="label"
            tickLine={false}
            axisLine={false}
            tick={<DayTick days={series} />}
          />
          <YAxis
            yAxisId="fluxo"
            tick={{ fontSize: 11, fill: chartColor.axis }}
            tickLine={false}
            axisLine={false}
            tickFormatter={brlAxisTick}
          />
          {/* A level and a movement cannot share an axis: a pharmacy holding
              R$ 40.000,00 against days that move R$ 1.200,00 would flatten
              every bar into the baseline. The right-hand axis is the balance's
              own, and it is coloured to say which line it belongs to. */}
          <YAxis
            yAxisId="saldo"
            orientation="right"
            tick={{ fontSize: 11, fill: chartColor.balance }}
            tickLine={false}
            axisLine={false}
            tickFormatter={brlAxisTick}
          />
          <Tooltip
            {...tooltipProps}
            formatter={v => formatBRL(Number(v ?? 0) * 100)}
            labelFormatter={(_, payload) => payload?.[0]?.payload?.full ?? ''}
          />
          <Legend wrapperStyle={{ fontSize: 12 }} iconType="circle" />

          {todayLabel && (
            <ReferenceLine
              yAxisId="fluxo"
              x={todayLabel}
              stroke={chartColor.today}
              strokeDasharray="4 4"
            />
          )}
          {/* Zero on the balance axis. Without it the line is a shape; with it,
              crossing it is the answer. */}
          <ReferenceLine yAxisId="saldo" y={0} stroke={chartColor.expense} strokeDasharray="2 3" />

          {/* One stack for the inflow: booked at the bottom, expected on top,
              so the uncertain money sits furthest from the axis. Outflow is a
              second bar beside it, never in the same stack. */}
          <Bar yAxisId="fluxo" dataKey="Entrada lançada" stackId="entra" fill={chartColor.income} maxBarSize={28} />
          <Bar yAxisId="fluxo" dataKey="Entrada esperada" stackId="entra" fill={chartColor.projected} radius={[4, 4, 0, 0]} maxBarSize={28} />
          {/* Only what is booked: the backend never invents an unbooked bill,
              because a guessed expense would soften an alarm on evidence
              nobody has. */}
          <Bar yAxisId="fluxo" dataKey="Saída" fill={chartColor.expense} radius={[4, 4, 0, 0]} maxBarSize={28}>
            {series.map((d) => (
              <Cell key={d.date} fillOpacity={d.isPast ? 0.55 : 1} />
            ))}
          </Bar>

          {/* Dashed end to end, and named "projetado" for the same reason: the
              days ahead are credited with an ordinary day's receipts, and even
              the anchor's balance counts a receivable that came due and may
              never have been paid. No part of this line is a bank statement. */}
          {/* Linear, not monotone: a balance is a value at the end of a day,
              and a smoothed curve between two of them draws amounts the
              account never held — on this fixture it would bulge above
              Friday's balance on the way down to Saturday's. */}
          <Line
            yAxisId="saldo"
            type="linear"
            dataKey="Saldo projetado"
            stroke={chartColor.balance}
            strokeWidth={2.5}
            strokeDasharray="6 4"
            dot={{ r: 3, fill: chartColor.balance, strokeWidth: 0 }}
          />
        </ComposedChart>
      </ResponsiveContainer>

      <p className="text-xs text-muted-foreground">
        Verde é o que já está lançado para entrar; âmbar é o recebimento médio
        daquele dia da semana, que ainda não é promessa de ninguém. A saída é só
        o que já está lançado — contas não lançadas não são estimadas. Ontem
        entra como referência e não tem âmbar: nada é estimado para um dia que
        já passou. A linha azul é o saldo no fim de cada dia — é ela que diz se
        uma saída grande é um susto ou um problema.
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
        {/* The window's own dip, named. cashPosition.daysUntilNegative answers
            this for the whole month; a card about a handful of days has to say
            whether the dip is inside them. */}
        {negative && (
          <span className="text-sm text-destructive">
            Fica negativo em {format(parseISO(negative.date), 'dd/MM')}
          </span>
        )}
      </div>
    </>
  )
}
