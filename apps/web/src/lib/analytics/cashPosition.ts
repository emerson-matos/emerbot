import type { CashFlowPoint } from '../../api/types'
import type { CashPosition } from './types'

export function getCashPosition(
  cashFlowPoints: CashFlowPoint[],
  now: Date,
): CashPosition {
  if (cashFlowPoints.length === 0) {
    return {
      currentBalance: 0,
      endOfMonthProjection: 0,
      daysUntilNegative: null,
      lowestProjected: 0,
      lowestProjectedDate: now.toISOString().slice(0, 10),
    }
  }

  const todayStr = now.toISOString().slice(0, 10)

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
      daysUntilNegative = Math.ceil(
        (new Date(point.Date).getTime() - now.getTime()) / (1000 * 60 * 60 * 24),
      )
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
