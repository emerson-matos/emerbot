import { describe, expect, it } from 'vitest'
import { makeEntry } from '@/test/factories'
import {
  activeFilterCount,
  buildGroups,
  compileFilters,
  defaultFilters,
  groupIntoMonthPages,
  ledgerTotals,
  toISORange,
  type FilterState,
} from './transaction-filters'

const TODAY = '2026-08-02'

function filters(overrides: Partial<FilterState> = {}): FilterState {
  return { ...defaultFilters, ...overrides }
}

function matcher(overrides: Partial<FilterState> = {}, categoryLabel?: (slug: string) => string) {
  return compileFilters(filters(overrides), { todayISO: TODAY, categoryLabel })
}

// Dates arrive from the calendar as local Date objects; the predicate compares
// ISO strings, so the conversion is where an off-by-one day would hide.
function day(iso: string): Date {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(y, m - 1, d)
}

describe('toISORange', () => {
  it('is null until a start date is picked', () => {
    expect(toISORange(undefined)).toBeNull()
    expect(toISORange({ from: undefined, to: undefined })).toBeNull()
  })

  it('leaves the end open when only one day is picked', () => {
    expect(toISORange({ from: day('2026-03-10'), to: undefined })).toEqual({
      from: '2026-03-10',
      to: undefined,
    })
  })

  it('formats both ends in the local calendar day, not UTC', () => {
    expect(toISORange({ from: day('2026-03-01'), to: day('2026-03-31') })).toEqual({
      from: '2026-03-01',
      to: '2026-03-31',
    })
  })
})

describe('compileFilters — period', () => {
  const range = { from: day('2026-03-01'), to: day('2026-03-31') }

  it('matches on DueDate when the entry has one', () => {
    // Registered in February, due in March: the period asks about the due date.
    const entry = makeEntry({ TransactionDate: '2026-02-20', DueDate: '2026-03-15' })
    expect(matcher({ date: range })(entry)).toBe(true)
  })

  it('falls back to TransactionDate when there is no DueDate', () => {
    const entry = makeEntry({ TransactionDate: '2026-03-15', DueDate: null })
    expect(matcher({ date: range })(entry)).toBe(true)
  })

  it('prefers DueDate over TransactionDate, so a settled February entry stays out of March', () => {
    const entry = makeEntry({ TransactionDate: '2026-03-15', DueDate: '2026-02-10' })
    expect(matcher({ date: range })(entry)).toBe(false)
  })

  it('includes both endpoints', () => {
    expect(matcher({ date: range })(makeEntry({ TransactionDate: '2026-03-01' }))).toBe(true)
    expect(matcher({ date: range })(makeEntry({ TransactionDate: '2026-03-31' }))).toBe(true)
    expect(matcher({ date: range })(makeEntry({ TransactionDate: '2026-02-28' }))).toBe(false)
    expect(matcher({ date: range })(makeEntry({ TransactionDate: '2026-04-01' }))).toBe(false)
  })

  it('keeps everything from the start date onwards when the end is open', () => {
    const open = { from: day('2026-03-10'), to: undefined }
    expect(matcher({ date: open })(makeEntry({ TransactionDate: '2026-03-09' }))).toBe(false)
    expect(matcher({ date: open })(makeEntry({ TransactionDate: '2027-12-31' }))).toBe(true)
  })

  it('drops entries with no date at all rather than guessing', () => {
    const entry = makeEntry({ TransactionDate: '', DueDate: null })
    expect(matcher({ date: range })(entry)).toBe(false)
  })
})

describe('compileFilters — status', () => {
  it('treats overdue as pending and past due, not merely past due', () => {
    const match = matcher({ status: 'overdue' })
    expect(match(makeEntry({ DueDate: '2026-07-01', PaymentStatus: 'pending' }))).toBe(true)
    expect(match(makeEntry({ DueDate: '2026-07-01', PaymentStatus: 'paid' }))).toBe(false)
  })

  it('does not count something due today as overdue', () => {
    const match = matcher({ status: 'overdue' })
    expect(match(makeEntry({ DueDate: TODAY, PaymentStatus: 'pending' }))).toBe(false)
  })

  it('matches the raw payment status for paid and pending', () => {
    const pending = makeEntry({ DueDate: '2026-07-01', PaymentStatus: 'pending' })
    expect(matcher({ status: 'pending' })(pending)).toBe(true)
    expect(matcher({ status: 'paid' })(pending)).toBe(false)
  })
})

describe('compileFilters — search', () => {
  const entry = makeEntry({ Description: 'Dipirona 500mg', Category: 'venda_balcao' })

  it('matches the description, case-insensitively', () => {
    expect(matcher({ search: 'DIPIRONA' })(entry)).toBe(true)
  })

  it('matches the category label the row actually shows', () => {
    const label = (slug: string) => (slug === 'venda_balcao' ? 'Venda Balcão' : slug)
    expect(matcher({ search: 'balcão' }, label)(entry)).toBe(true)
  })

  it('ignores surrounding whitespace', () => {
    expect(matcher({ search: '  dipirona  ' })(entry)).toBe(true)
  })

  it('rejects a term in neither field', () => {
    expect(matcher({ search: 'amoxicilina' })(entry)).toBe(false)
  })
})

describe('compileFilters — amount', () => {
  const entry = makeEntry({ Amount: 5000 })

  it('compares in centavos', () => {
    expect(matcher({ minAmount: '50,00' })(entry)).toBe(true)
    expect(matcher({ minAmount: '50,01' })(entry)).toBe(false)
    expect(matcher({ maxAmount: '50,00' })(entry)).toBe(true)
    expect(matcher({ maxAmount: '49,99' })(entry)).toBe(false)
  })

  // A typo must not read as "no results" — the input is flagged instead.
  it('does not constrain the list when the field cannot be parsed', () => {
    expect(matcher({ minAmount: 'abc' })(entry)).toBe(true)
    expect(matcher({ maxAmount: 'abc' })(entry)).toBe(true)
  })
})

describe('compileFilters — combinations', () => {
  it('requires every active filter to pass', () => {
    const match = matcher({ type: 'expense', status: 'pending', category: 'fornecedor' })
    const base = {
      Type: 'expense' as const,
      PaymentStatus: 'pending' as const,
      Category: 'fornecedor',
      DueDate: '2026-09-01',
    }
    expect(match(makeEntry(base))).toBe(true)
    expect(match(makeEntry({ ...base, Category: 'aluguel' }))).toBe(false)
    expect(match(makeEntry({ ...base, Type: 'income' }))).toBe(false)
  })

  it('matches everything when no filter is set', () => {
    expect(matcher()(makeEntry())).toBe(true)
  })
})

describe('activeFilterCount', () => {
  it('is zero for the defaults', () => {
    expect(activeFilterCount(defaultFilters)).toBe(0)
  })

  it('counts each field the user set', () => {
    expect(activeFilterCount(filters({ search: 'x' }))).toBe(1)
    expect(activeFilterCount(filters({ search: 'x', type: 'income', minAmount: '10' }))).toBe(3)
    expect(activeFilterCount(filters({ date: { from: day('2026-03-01'), to: undefined } }))).toBe(1)
  })
})

describe('groupIntoMonthPages', () => {
  it('buckets a flat range response by effective month', () => {
    const pages = groupIntoMonthPages([
      makeEntry({ EntryID: 'a', TransactionDate: '2026-03-05' }),
      makeEntry({ EntryID: 'b', TransactionDate: '2026-03-20' }),
      makeEntry({ EntryID: 'c', TransactionDate: '2026-04-01' }),
    ])
    expect(pages).toHaveLength(2)
    expect(pages.find(p => p.month === '2026-03')?.entries.map(e => e.EntryID)).toEqual(['a', 'b'])
    expect(pages.find(p => p.month === '2026-04')?.entries.map(e => e.EntryID)).toEqual(['c'])
  })

  it('buckets by DueDate, so an entry lands in the month it is due', () => {
    const pages = groupIntoMonthPages([
      makeEntry({ EntryID: 'a', TransactionDate: '2026-02-25', DueDate: '2026-03-10' }),
    ])
    expect(pages.map(p => p.month)).toEqual(['2026-03'])
  })

  it('returns nothing for an empty response', () => {
    expect(groupIntoMonthPages([])).toEqual([])
  })
})

describe('buildGroups', () => {
  const currentMonth = '2026-08'
  const matchAll = () => true

  it('orders future months ascending, then the current month, then past descending', () => {
    const pages = [
      { month: '2026-06', entries: [makeEntry({ EntryID: 'jun', TransactionDate: '2026-06-10' })] },
      { month: '2026-09', entries: [makeEntry({ EntryID: 'set', TransactionDate: '2026-09-10' })] },
      { month: '2026-07', entries: [makeEntry({ EntryID: 'jul', TransactionDate: '2026-07-10' })] },
      { month: '2026-10', entries: [makeEntry({ EntryID: 'out', TransactionDate: '2026-10-10' })] },
    ]
    expect(buildGroups(pages, currentMonth, TODAY, matchAll).map(g => g.key)).toEqual([
      '2026-09', '2026-10', '2026-07', '2026-06',
    ])
  })

  it('splits the current month by urgency', () => {
    const pages = [{
      month: currentMonth,
      entries: [
        makeEntry({ EntryID: 'atrasado', DueDate: '2026-08-01', PaymentStatus: 'pending' }),
        makeEntry({ EntryID: 'hoje', DueDate: TODAY, PaymentStatus: 'pending' }),
        makeEntry({ EntryID: 'futuro', DueDate: '2026-08-20', PaymentStatus: 'pending' }),
        makeEntry({ EntryID: 'quitado', DueDate: '2026-08-01', PaymentStatus: 'paid' }),
      ],
    }]
    expect(buildGroups(pages, currentMonth, TODAY, matchAll).map(g => g.key)).toEqual([
      'overdue', 'today', 'upcoming', 'history',
    ])
  })

  it('anchors the current month at the top, ahead of future and past blocks', () => {
    const pages = [
      { month: '2026-06', entries: [makeEntry({ EntryID: 'jun', TransactionDate: '2026-06-10' })] },
      { month: '2026-09', entries: [makeEntry({ EntryID: 'set', TransactionDate: '2026-09-10' })] },
      {
        month: currentMonth,
        entries: [makeEntry({ EntryID: 'ago', DueDate: '2026-08-20', PaymentStatus: 'pending' })],
      },
    ]
    expect(buildGroups(pages, currentMonth, TODAY, matchAll).map(g => g.key)).toEqual([
      'upcoming', '2026-09', '2026-06',
    ])
  })

  it('drops groups whose entries are all filtered out', () => {
    const pages = [
      { month: '2026-06', entries: [makeEntry({ EntryID: 'jun', Type: 'income' })] },
      { month: '2026-07', entries: [makeEntry({ EntryID: 'jul', Type: 'expense' })] },
    ]
    const onlyExpenses = compileFilters(filters({ type: 'expense' }), { todayISO: TODAY })
    expect(buildGroups(pages, currentMonth, TODAY, onlyExpenses).map(g => g.key)).toEqual(['2026-07'])
  })

  // The whole point of routing the range response through groupIntoMonthPages:
  // a period query renders exactly like the month-paged browse view.
  it('renders a range response the same way as month pages', () => {
    const entries = [
      makeEntry({ EntryID: 'a', TransactionDate: '2026-03-05' }),
      makeEntry({ EntryID: 'b', TransactionDate: '2026-04-05' }),
    ]
    const fromRange = buildGroups(groupIntoMonthPages(entries), currentMonth, TODAY, matchAll)
    const fromPages = buildGroups(
      [
        { month: '2026-03', entries: [entries[0]] },
        { month: '2026-04', entries: [entries[1]] },
      ],
      currentMonth, TODAY, matchAll,
    )
    expect(fromRange.map(g => g.key)).toEqual(fromPages.map(g => g.key))
    expect(fromRange.map(g => g.label)).toEqual(fromPages.map(g => g.label))
  })
})

describe('ledgerTotals', () => {
  it('splits income from expense and nets them', () => {
    expect(ledgerTotals([
      makeEntry({ Type: 'income', Amount: 10000 }),
      makeEntry({ Type: 'income', Amount: 2500 }),
      makeEntry({ Type: 'expense', Amount: 4000 }),
    ])).toEqual({ count: 3, entradas: 12500, saidas: 4000, saldo: 8500 })
  })

  it('is all zeros for an empty selection', () => {
    expect(ledgerTotals([])).toEqual({ count: 0, entradas: 0, saidas: 0, saldo: 0 })
  })
})
