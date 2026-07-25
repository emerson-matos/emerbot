import type { Entry } from '../../api/types'
import type { WeekdayStat } from './types'

const WEEKDAY_LABELS = [
  'Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb',
]

export function getWeekdayStats(
  entries: Entry[],
  now: Date,
): WeekdayStat[] {
  const today = now.getDay()

  const byDay = new Map<number, { total: number; count: number; dates: Set<string> }>()

  for (let d = 0; d < 7; d++) {
    byDay.set(d, { total: 0, count: 0, dates: new Set() })
  }

  for (const e of entries) {
    if (e.Type !== 'income' || e.Category !== 'venda_balcao') continue
    const dateStr = e.TransactionDate.slice(0, 10)
    const date = new Date(dateStr + 'T12:00:00')
    const day = date.getDay()
    const bucket = byDay.get(day)!
    bucket.total += e.Amount
    bucket.dates.add(dateStr)
  }

  const stats: WeekdayStat[] = []
  for (let d = 0; d < 7; d++) {
    const bucket = byDay.get(d)!
    const count = bucket.dates.size
    stats.push({
      day: d,
      label: WEEKDAY_LABELS[d],
      avg: count > 0 ? Math.round(bucket.total / count) : 0,
      total: bucket.total,
      count,
      isToday: d === today,
    })
  }

  return stats
}
