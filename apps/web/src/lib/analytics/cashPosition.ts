import { differenceInCalendarDays, format, parseISO } from 'date-fns'
import type { CashFlowPoint } from '../../api/types'
import type { CashPosition } from './types'

export function getCashPosition(
  cashFlowPoints: CashFlowPoint[],
  now: Date,
): CashPosition {
  // CashFlowPoint.Date is a plain calendar day, so "today" has to be the
  // user's local day. toISOString() would roll over to tomorrow during the
  // evening in any UTC- timezone and lose the current balance entirely.
  const todayStr = format(now, 'yyyy-MM-dd')

  if (cashFlowPoints.length === 0) {
    return {
      currentBalance: 0,
      endOfMonthProjection: 0,
      daysUntilNegative: null,
      lowestProjected: 0,
      lowestProjectedDate: todayStr,
    }
  }

  const todayPoint = cashFlowPoints.find(p => p.Date === todayStr)
  const currentBalance = todayPoint?.RunningBalance ?? 0

  const lastPoint = cashFlowPoints[cashFlowPoints.length - 1]
  const endOfMonthProjection = lastPoint?.RunningBalance ?? 0

  let lowestProjected = currentBalance
  let lowestProjectedDate = todayStr
  let daysUntilNegative: number | null = null

  for (const point of cashFlowPoints) {
    if (point.RunningBalance < lowestProjected) {
      lowestProjected = point.RunningBalance
      lowestProjectedDate = point.Date
    }
    if (point.RunningBalance < 0 && daysUntilNegative === null && point.Date > todayStr) {
      // Calendar days apart, not elapsed milliseconds — otherwise the answer
      // depends on the time of day the dashboard happens to be open.
      daysUntilNegative = differenceInCalendarDays(parseISO(point.Date), now)
    }
  }

  return {
    currentBalance,
    endOfMonthProjection,
    daysUntilNegative,
    lowestProjected,
    lowestProjectedDate,
  }
}
