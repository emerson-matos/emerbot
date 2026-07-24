import { format } from 'date-fns'

// currentMonthRange is the single source of the "this month" window the
// Adquirentes page and its KPI cards query with. Keeping one copy matters
// because these values are React Query cache keys: two components computing the
// range separately can straddle a month boundary and render disagreeing totals.
export function currentMonthRange() {
  const now = new Date()
  return {
    month: format(now, 'yyyy-MM'),
    firstDay: format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd'),
    lastDay: format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd'),
  }
}
