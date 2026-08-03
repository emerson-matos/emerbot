import { format } from 'date-fns'
import { currentMonthKey } from './entries'

// monthRange turns a "yyyy-MM" key into the first/last calendar day of that
// month. It is the single source of the window the Adquirentes page and its KPI
// cards query with: these values are React Query cache keys, so computing the
// range in one place keeps every card agreeing on the same period.
export function monthRange(monthKey: string) {
  const [year, month] = monthKey.split('-').map(Number)
  return {
    month: monthKey,
    firstDay: format(new Date(year, month - 1, 1), 'yyyy-MM-dd'),
    lastDay: format(new Date(year, month, 0), 'yyyy-MM-dd'),
  }
}

// currentMonthRange is monthRange for the month we're in now (the pharmacy's
// calendar, via currentMonthKey — not the browser's local timezone) — the
// default the Adquirentes page opens on before the user picks another month.
export function currentMonthRange() {
  return monthRange(currentMonthKey())
}
