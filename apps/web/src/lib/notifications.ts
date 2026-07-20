import { format } from 'date-fns'
import { AlertTriangle, CalendarClock, Trophy } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { formatBRL } from '../api/client'
import { effectiveDate, formatEffectiveDate } from './entries'
import { useEntries, useGoal, useMonthlySummary } from '../api/queries'

export type NotificationTone = 'warning' | 'destructive' | 'success' | 'info'

export interface AppNotification {
  id: string
  icon: LucideIcon
  tone: NotificationTone
  text: string
  time: string
}

export const notificationToneVar: Record<NotificationTone, string> = {
  warning: 'var(--warning)',
  destructive: 'var(--destructive)',
  success: 'var(--success)',
  info: 'var(--info)',
}

// How far back to look for still-pending expenses when flagging overdue bills.
const OVERDUE_LOOKBACK_MONTHS = 3
// Cap the overdue feed so a large backlog can't flood the bell popover.
const MAX_OVERDUE = 3

interface NotificationsResult {
  notifications: AppNotification[]
  hasNotifications: boolean
  isLoading: boolean
}

// Derives the alert feed entirely on the client from data the dashboard already
// caches — no dedicated backend. Three sources, matching the design mock:
//   • a payment due today, • overdue pending expenses, • the revenue goal hit.
// Phase 2 (docs/notifications-phase-2.md) adds server-side WhatsApp delivery.
export function useNotifications(): NotificationsResult {
  const now = new Date()
  const today = format(now, 'yyyy-MM-dd')
  const currentMonth = format(now, 'yyyy-MM')
  const windowStart = format(
    new Date(now.getFullYear(), now.getMonth() - OVERDUE_LOOKBACK_MONTHS, 1),
    'yyyy-MM-dd',
  )

  // The API bounds entries by effectiveDate (DueDate when set, else Date — see
  // packages/finance/store.go), so this single range covers both "vence hoje"
  // and the overdue backlog without extra requests.
  const entriesQuery = useEntries(windowStart, today)
  const summaryQuery = useMonthlySummary(currentMonth)
  const goalQuery = useGoal(currentMonth)

  const entries = entriesQuery.data?.entries ?? []
  const pendingExpenses = entries.filter(
    e => e.Type === 'expense' && e.PaymentStatus === 'pending',
  )

  const notifications: AppNotification[] = []

  const dueTodayTotal = pendingExpenses
    .filter(e => effectiveDate(e)?.slice(0, 10) === today)
    .reduce((sum, e) => sum + e.Amount, 0)
  if (dueTodayTotal > 0) {
    notifications.push({
      id: 'due-today',
      icon: CalendarClock,
      tone: 'warning',
      text: `Pagamento de ${formatBRL(dueTodayTotal)} vence hoje`,
      time: 'Hoje',
    })
  }

  pendingExpenses
    .filter(e => (effectiveDate(e)?.slice(0, 10) ?? '') < today)
    .sort((a, b) => (effectiveDate(b) ?? '').localeCompare(effectiveDate(a) ?? ''))
    .slice(0, MAX_OVERDUE)
    .forEach(e => {
      notifications.push({
        id: `overdue-${e.EntryID}`,
        icon: AlertTriangle,
        tone: 'destructive',
        text: `${e.Description || 'Conta'} está vencida`,
        time: formatEffectiveDate(e),
      })
    })

  const summary = summaryQuery.data
  const goal = goalQuery.data?.goal
  if (
    goal && goal.RevenueTarget > 0 &&
    summary && summary.TotalIncome >= goal.RevenueTarget
  ) {
    notifications.push({
      id: 'goal-reached',
      icon: Trophy,
      tone: 'success',
      text: 'Meta de faturamento atingida!',
      time: 'Este mês',
    })
  }

  return {
    notifications,
    hasNotifications: notifications.length > 0,
    isLoading:
      entriesQuery.isLoading || summaryQuery.isLoading || goalQuery.isLoading,
  }
}
