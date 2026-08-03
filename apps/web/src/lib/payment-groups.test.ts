import { describe, expect, it } from 'vitest'
import { makeEntry } from '@/test/factories'
import { urgencyGroups } from './payment-groups'

const TODAY = '2026-02-05'

describe('urgencyGroups', () => {
  const overdue = makeEntry({ EntryID: 'overdue', DueDate: '2026-01-20', PaymentStatus: 'pending' })
  const dueToday = makeEntry({ EntryID: 'today', DueDate: TODAY, PaymentStatus: 'pending' })
  const upcoming = makeEntry({ EntryID: 'upcoming', DueDate: '2026-02-20', PaymentStatus: 'pending' })
  const history = makeEntry({ EntryID: 'history', DueDate: '2026-01-20', PaymentStatus: 'paid' })

  /**
   * The order is the point of this function. The Painel and the Transações page
   * used to assemble these groups separately and disagreed — one led with the
   * overdue, the other buried it third — so the same ledger read differently
   * depending on which page you were on.
   */
  it('always orders the groups from most to least urgent', () => {
    const groups = urgencyGroups([history, upcoming, dueToday, overdue], TODAY)

    expect(groups.map(g => g.key)).toEqual(['overdue', 'today', 'upcoming', 'history'])
  })

  it('omits groups with nothing in them', () => {
    const groups = urgencyGroups([upcoming], TODAY)

    expect(groups.map(g => g.key)).toEqual(['upcoming'])
  })

  it('returns nothing for no entries', () => {
    expect(urgencyGroups([], TODAY)).toEqual([])
  })

  it('labels the today group with the day it describes', () => {
    const groups = urgencyGroups([dueToday], TODAY)

    expect(groups[0].label).toBe('Hoje · 05/02')
  })

  it('carries the tone each group is rendered with', () => {
    const groups = urgencyGroups([overdue, dueToday, upcoming, history], TODAY)

    expect(groups.map(g => g.tone)).toEqual(['negative', 'warning', 'info', 'neutral'])
  })
})
