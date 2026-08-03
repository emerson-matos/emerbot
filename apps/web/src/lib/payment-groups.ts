import { format } from 'date-fns'
import type { PaymentGroupData } from '@/components/payments/PaymentGroup'
import { bucketByUrgency } from '@/lib/entries'
import type { Entry } from '@/api/types'

/**
 * The urgency groups, always in the same order: what is already late, what is
 * due today, what is coming, then what is settled.
 *
 * This lives in one place because it used to live in two. The Painel and the
 * Transações page each assembled these four groups by hand, with their own copy
 * of the labels and tones — and they disagreed about the order, so the same
 * ledger read as a triage list on one page and a timeline on the other. The
 * order is a property of the grouping, not of whoever renders it.
 *
 * Empty groups are dropped. Callers that hide the history behind a toggle
 * filter it out by key and read its count from the group itself.
 */
export function urgencyGroups(entries: Entry[], todayISO: string): PaymentGroupData[] {
  const { overdue, dueToday, upcoming, history } = bucketByUrgency(entries, todayISO)
  const groups: PaymentGroupData[] = []

  if (overdue.length) {
    groups.push({ key: 'overdue', label: 'Em atraso', kind: 'status', tone: 'negative', items: overdue })
  }
  if (dueToday.length) {
    // The label reads off todayISO, not a fresh Date: the group and the day it
    // claims to describe have to be the same day.
    const day = format(new Date(`${todayISO}T00:00:00`), 'dd/MM')
    groups.push({ key: 'today', label: `Hoje · ${day}`, kind: 'status', tone: 'warning', items: dueToday })
  }
  if (upcoming.length) {
    groups.push({ key: 'upcoming', label: 'Próximos vencimentos', kind: 'status', tone: 'info', items: upcoming })
  }
  if (history.length) {
    groups.push({ key: 'history', label: 'Histórico do mês', kind: 'status', tone: 'neutral', items: history })
  }

  return groups
}
