import { format } from 'date-fns'
import { AlertTriangle, CalendarClock, Trophy } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { formatBRL } from '@/lib/format'
import { effectiveDate, formatEffectiveDate } from './entries'
import { useEntries, useGoal } from '../api/queries'
import type { Entry } from '../api/types'

export type NotificationTone = 'warning' | 'destructive' | 'success' | 'info'

export interface AppNotification {
  id: string
  icon: LucideIcon
  tone: NotificationTone
  text: string
  time: string
  link?: string
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
//   • a payment due today, • overdue pending expenses, • the faturamento goal hit.
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
      link: '/analise',
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
        link: '/analise',
      })
    })

  const goal = goalQuery.data?.goal
  // entries spans the overdue lookback window, several months wide — the goal
  // alert must count only this month's faturamento: a loan or aporte, or a sale
  // from an earlier month, must not trigger "meta atingida".
  //
  // A sale is decided by Origin, not by category. The Origin === undefined arm
  // is the same migration shim as domain.IsRevenue: entries written before the
  // field existed fall back to the old category rule. Delete it together with
  // the Go one, once scripts/migrate-origin has run.
  //
  // Bucketed by the transaction date, not the effective date: a sale belongs to
  // the month it was made, so a crediário sale counts toward this month's goal
  // even though it is due next month.
  const isSale = (e: Entry) =>
    e.Type === 'income'
    && (e.Origin === undefined || e.Origin === '' ? e.Category !== 'outros_receitas' : e.Origin === 'venda')
  const faturamentoThisMonth = entries
    .filter(e => isSale(e) && e.TransactionDate.slice(0, 7) === currentMonth)
    .reduce((sum, e) => sum + e.Amount, 0)
  if (
    goal && goal.RevenueTarget > 0 &&
    faturamentoThisMonth >= goal.RevenueTarget
  ) {
    notifications.push({
      id: 'goal-reached',
      icon: Trophy,
      tone: 'success',
      text: 'Meta de faturamento atingida!',
      time: 'Este mês',
      link: '/analise',
    })
  }

  return {
    notifications,
    hasNotifications: notifications.length > 0,
    isLoading:
      entriesQuery.isLoading || goalQuery.isLoading,
  }
}
