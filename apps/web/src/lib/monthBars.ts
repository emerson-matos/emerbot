import type { Analysis } from "@/api/types";

/** One month of the chart, in reais — Recharts reads these keys by name. */
export interface MonthBars {
  name: string
  Faturamento: number
  'Projeção': number
  Despesas: number
}

/**
 * Builds the three bars per month. Only the last slot is the month still being
 * traded; the two before it have closed and have nothing left to project —
 * drawing an amber segment for them would invent a future for a month that has
 * none.
 *
 * `projected` is never below `actual` (the days ahead can only add to it), and
 * both come from the same faturamento the green bar is drawn from, so the stack
 * lands exactly on the projected total. It is clamped anyway: a negative
 * segment would render as a bar growing downward out of the axis.
 */
export function monthBars(
  months: { month: string; revenue: number; expense: number }[],
  projection?: Pick<Analysis['projection'], 'actual' | 'projected'>,
): MonthBars[] {
  return months.map((d, i) => ({
    name: d.month,
    Faturamento: d.revenue / 100,
    'Projeção': i === months.length - 1 && projection
      ? Math.max(0, projection.projected - projection.actual) / 100
      : 0,
    Despesas: d.expense / 100,
  }))
}
