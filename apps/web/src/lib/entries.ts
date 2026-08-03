import { format, isValid, parseISO } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import type { Entry } from '../api/types'

// Tables are about *due* transactions: pending entries show when they're
// due, and already-settled ones (no DueDate) fall back to when they happened.
export function effectiveDate(e: Entry): string | null {
  return e.DueDate || e.TransactionDate || null
}

export function formatEffectiveDate(e: Entry): string {
  const iso = effectiveDate(e)
  if (!iso) return '—'
  const parsed = parseISO(iso)
  return isValid(parsed) ? format(parsed, 'dd/MM/yy', { locale: ptBR }) : '—'
}

export function formatPaidAt(e: Entry): string {
  if (!e.PaymentDate) return ''
  const parsed = parseISO(e.PaymentDate)
  return isValid(parsed) ? `em ${format(parsed, 'dd/MM', { locale: ptBR })}` : ''
}

/**
 * Where an entry is edited. The transaction date is in the path because it is
 * half the entry's address in the API (see api.entries) — an id alone does not
 * identify a row. A date edit therefore changes this path, which is why the
 * edit page navigates away on save rather than staying put.
 */
export function editEntryPath(e: Entry): string {
  return `/transacoes/${e.TransactionDate}/${e.EntryID}/editar`
}

export function netAmount(entries: Entry[]): number {
  return entries.reduce((sum, e) => sum + (e.Type === 'income' ? e.Amount : -e.Amount), 0)
}

export type SortDirection = 'asc' | 'desc'

/**
 * Orders entries by effective date, then by amount descending, then by id.
 *
 * The last two keys are what make the order *total*: dates repeat constantly —
 * a dozen invoices can fall due on the same day — and a comparator that only
 * looks at the date leaves those rows free to land in any order, so the list
 * reshuffles itself on every refetch. Amount decides what a reader would ask
 * for next (the biggest obligation first) and the id guarantees there is always
 * a tiebreak left.
 */
export function compareEntries(a: Entry, b: Entry, direction: SortDirection = 'desc'): number {
  const da = effectiveDate(a) ?? ''
  const db = effectiveDate(b) ?? ''
  if (da !== db) return direction === 'asc' ? da.localeCompare(db) : db.localeCompare(da)
  if (a.Amount !== b.Amount) return b.Amount - a.Amount
  return a.EntryID.localeCompare(b.EntryID)
}

export function sortEntries(entries: Entry[], direction: SortDirection = 'desc'): Entry[] {
  return [...entries].sort((a, b) => compareEntries(a, b, direction))
}

export interface UrgencyBuckets {
  overdue: Entry[]
  dueToday: Entry[]
  upcoming: Entry[]
  history: Entry[]
}

/**
 * Splits entries relative to today: overdue (pending, past due), due today
 * (any status), upcoming (any status, future-dated), and history (already
 * paid, past-dated — kept separate so callers can hide it by default).
 *
 * Each bucket is sorted here rather than inherited from the caller. Overdue
 * runs oldest first, because the debt that has been unpaid longest is the one
 * that needs attention, and upcoming runs soonest first for the same reason
 * read forwards; history is most recent first, the way a statement is read.
 */
export function bucketByUrgency(entries: Entry[], todayISO: string): UrgencyBuckets {
  const overdue: Entry[] = []
  const dueToday: Entry[] = []
  const upcoming: Entry[] = []
  const history: Entry[] = []
  for (const e of entries) {
    const date = effectiveDate(e) ?? ''
    if (date === todayISO) dueToday.push(e)
    else if (date > todayISO) upcoming.push(e)
    else if (e.PaymentStatus === 'pending') overdue.push(e)
    else history.push(e)
  }
  return {
    overdue: sortEntries(overdue, 'asc'),
    dueToday: sortEntries(dueToday, 'asc'),
    upcoming: sortEntries(upcoming, 'asc'),
    history: sortEntries(history, 'desc'),
  }
}
