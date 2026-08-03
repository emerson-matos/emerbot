import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import type { DateRange } from 'react-day-picker'
import type { PaymentGroupData } from '@/components/payments/PaymentGroup'
import { bucketByUrgency, effectiveDate } from '@/lib/entries'
import { parseAmountToCents } from '@/lib/format'
import type { Entry, IncomeOrigin } from '@/api/types'

export type TypeFilter = 'all' | 'income' | 'expense'
export type OriginFilter = 'all' | IncomeOrigin
export type StatusFilter = 'all' | 'paid' | 'pending' | 'overdue'

export type FilterState = {
  search: string
  type: TypeFilter
  status: StatusFilter
  category: string
  origin: OriginFilter
  date: DateRange | undefined
  minAmount: string
  maxAmount: string
}

export const defaultFilters: FilterState = {
  search: '',
  type: 'all',
  status: 'all',
  category: 'all',
  origin: 'all',
  date: undefined,
  minAmount: '',
  maxAmount: '',
}

// The period is the one filter the client cannot answer on its own. Every other
// field narrows rows we already hold; a period asks for rows that may never have
// been fetched. So it is turned into an ISO window and handed to the server —
// GET /entries?from&to ranges over the same effectiveDate the predicate below
// compares against, and drops its row limit when a range is given.
//
// `to` stays optional: picking a single day in the range calendar means "from
// that day onwards", which is how the predicate and the server both read it.
export type ISORange = { from: string; to?: string }

export function toISORange(date: DateRange | undefined): ISORange | null {
  if (!date?.from) return null
  return {
    from: format(date.from, 'yyyy-MM-dd'),
    to: date.to ? format(date.to, 'yyyy-MM-dd') : undefined,
  }
}

export interface FilterContext {
  todayISO: string
  // Resolves a category slug to its label, so search matches the text the row
  // actually shows — the search box has always promised categories.
  categoryLabel?: (slug: string) => string
}

// Compiles the applied filters into a predicate once per change, rather than
// re-parsing amounts and re-formatting dates for every entry in the list.
//
// Amounts are compared in centavos, the unit the API stores. An unparseable
// field parses to null and simply doesn't constrain the list — the input is
// flagged instead, so a typo never looks like "no results".
export function compileFilters(f: FilterState, ctx: FilterContext): (e: Entry) => boolean {
  const range = toISORange(f.date)
  const minCents = parseAmountToCents(f.minAmount)
  const maxCents = parseAmountToCents(f.maxAmount)
  const needle = f.search.trim().toLowerCase()
  const labelOf = ctx.categoryLabel ?? ((slug: string) => slug)

  return (e: Entry) => {
    if (f.type !== 'all' && e.Type !== f.type) return false
    if (f.status === 'overdue') {
      if (e.PaymentStatus !== 'pending' || (effectiveDate(e) ?? '') >= ctx.todayISO) return false
    } else if (f.status !== 'all' && e.PaymentStatus !== f.status) return false
    if (f.category !== 'all' && e.Category !== f.category) return false
    if (f.origin !== 'all' && e.Origin !== f.origin) return false
    if (range) {
      const d = effectiveDate(e)
      if (!d) return false
      if (d < range.from) return false
      if (range.to && d > range.to) return false
    }
    if (minCents !== null && e.Amount < minCents) return false
    if (maxCents !== null && e.Amount > maxCents) return false
    if (needle && !`${e.Description} ${labelOf(e.Category)}`.toLowerCase().includes(needle)) return false
    return true
  }
}

// Counts the filters a user actually set. "Limpar" reads this over the pending
// state as well as the applied one, so a form that was edited but never applied
// can still be reset.
export function activeFilterCount(f: FilterState): number {
  let n = 0
  if (f.search !== '') n++
  if (f.type !== 'all') n++
  if (f.status !== 'all') n++
  if (f.category !== 'all') n++
  if (f.origin !== 'all') n++
  if (f.date !== undefined) n++
  if (f.minAmount !== '') n++
  if (f.maxAmount !== '') n++
  return n
}

// An entry the calendar cannot place gets a label instead of a crash: date-fns
// throws on an invalid date, and a group header is no place to take that risk.
export function monthLabel(monthKey: string): string {
  const date = new Date(`${monthKey}-01T00:00:00`)
  if (Number.isNaN(date.getTime())) return 'Sem data'
  return format(date, 'MMMM yyyy', { locale: ptBR })
}

export function formatRangeLabel(range: ISORange): string {
  const day = (iso: string) => format(new Date(`${iso}T00:00:00`), 'dd/MM/yy')
  return range.to ? `${day(range.from)} – ${day(range.to)}` : `a partir de ${day(range.from)}`
}

export interface MonthPage {
  month: string
  entries: Entry[]
}

// Buckets a flat list into the same one-month-per-page shape the infinite query
// produces. This is what lets a server-side range response go through the very
// same grouping as month browsing, so both modes render identically.
export function groupIntoMonthPages(entries: Entry[]): MonthPage[] {
  const byMonth = new Map<string, Entry[]>()
  for (const e of entries) {
    const month = (effectiveDate(e) ?? '').slice(0, 7)
    const bucket = byMonth.get(month)
    if (bucket) bucket.push(e)
    else byMonth.set(month, [e])
  }
  return [...byMonth.entries()].map(([month, monthEntries]) => ({ month, entries: monthEntries }))
}

// Orders the page: months ahead of the current one ascending, the current month
// split by urgency, then past months descending — nearest to today, outward.
export function buildGroups(
  pages: MonthPage[],
  currentMonthKey: string,
  todayISO: string,
  matches: (e: Entry) => boolean,
): PaymentGroupData[] {
  const result: PaymentGroupData[] = []

  const future = pages.filter(p => p.month > currentMonthKey).sort((a, b) => a.month.localeCompare(b.month))
  for (const p of future) {
    const items = p.entries.filter(matches)
    if (items.length) {
      result.push({ key: p.month, label: monthLabel(p.month), kind: 'period', tone: 'info', items })
    }
  }

  const current = pages.find(p => p.month === currentMonthKey)
  if (current) {
    const { overdue, dueToday, upcoming, history } = bucketByUrgency(current.entries.filter(matches), todayISO)
    if (upcoming.length) {
      result.push({ key: 'upcoming', label: 'Próximos vencimentos', kind: 'status', tone: 'info', items: upcoming })
    }
    if (dueToday.length) {
      const day = format(new Date(`${todayISO}T00:00:00`), 'dd/MM')
      result.push({ key: 'today', label: `Hoje · ${day}`, kind: 'status', tone: 'warning', items: dueToday })
    }
    if (overdue.length) {
      result.push({ key: 'overdue', label: 'Em atraso', kind: 'status', tone: 'negative', items: overdue })
    }
    if (history.length) {
      result.push({ key: 'history', label: 'Histórico do mês', kind: 'status', tone: 'neutral', items: history })
    }
  }

  const past = pages.filter(p => p.month < currentMonthKey).sort((a, b) => b.month.localeCompare(a.month))
  for (const p of past) {
    const items = p.entries.filter(matches)
    if (items.length) {
      result.push({ key: p.month, label: monthLabel(p.month), kind: 'period', tone: 'neutral', items })
    }
  }

  return result
}

export interface LedgerTotals {
  count: number
  entradas: number
  saidas: number
  saldo: number
}

// The ledger strip splits the filtered set the way a caixa is read: money in,
// money out, and the balance between them.
export function ledgerTotals(entries: Entry[]): LedgerTotals {
  let entradas = 0
  let saidas = 0
  for (const e of entries) {
    if (e.Type === 'income') entradas += e.Amount
    else saidas += e.Amount
  }
  return { count: entries.length, entradas, saidas, saldo: entradas - saidas }
}
