import { describe, expect, it } from 'vitest'
import { makeEntry } from '@/test/factories'
import {
  amountToCents,
  centsToAmountField,
  editEntryPatch,
  entryFormErrors,
  entryToEditValues,
  isEmptyPatch,
  type EditEntryValues,
} from './entry-form'

describe('amountToCents', () => {
  it('converts reais to the centavos the API stores', () => {
    expect(amountToCents('1234.56')).toBe(123456)
    expect(amountToCents('10')).toBe(1000)
  })

  // Floating point makes 19.99 * 100 land at 1998.9999…; truncating would lose
  // a centavo on the commonest price shape there is.
  it('rounds instead of truncating', () => {
    expect(amountToCents('19.99')).toBe(1999)
  })

  it('rejects anything that is not a positive amount', () => {
    for (const raw of ['', '   ', 'abc', '0', '-5']) {
      expect(amountToCents(raw)).toBeNull()
    }
  })

  it('round-trips through the form field', () => {
    expect(amountToCents(centsToAmountField(123456))).toBe(123456)
  })
})

describe('entryFormErrors', () => {
  const valid = {
    type: 'expense' as const,
    description: 'Aluguel',
    category: 'aluguel',
    amount: '100',
    date: '2026-07-10',
  }

  it('accepts a complete entry', () => {
    expect(Object.values(entryFormErrors(valid)).some(Boolean)).toBe(false)
  })

  it('flags each missing field', () => {
    expect(entryFormErrors({ ...valid, description: '   ' }).description).toBe(true)
    expect(entryFormErrors({ ...valid, category: '' }).category).toBe(true)
    expect(entryFormErrors({ ...valid, amount: '0' }).amount).toBe(true)
    expect(entryFormErrors({ ...valid, date: '' }).date).toBe(true)
  })
})

describe('editEntryPatch', () => {
  function values(overrides: Partial<EditEntryValues> = {}): EditEntryValues {
    return {
      ...entryToEditValues(
        makeEntry({
          Amount: 10000,
          Category: 'venda_balcao',
          Description: 'Venda balcão',
          TransactionDate: '2026-07-10',
          Origin: 'venda',
        }),
      ),
      ...overrides,
    }
  }

  it('sends nothing when nothing changed', () => {
    expect(isEmptyPatch(editEntryPatch(values(), values()))).toBe(true)
  })

  it('sends only the field that changed', () => {
    const patch = editEntryPatch(values(), values({ description: 'Venda balcão corrigida' }))
    expect(patch).toEqual({ description: 'Venda balcão corrigida' })
  })

  it('converts the amount to centavos', () => {
    expect(editEntryPatch(values(), values({ amount: '150.50' }))).toEqual({ amount: 15050 })
  })

  it('sends an emptied due date, which is how the API is told to clear it', () => {
    const before = values({ dueDate: '2026-07-20' })
    expect(editEntryPatch(before, values({ dueDate: '' }))).toEqual({ due_date: '' })
  })

  /**
   * An entry written before Origin existed has none, and domain.IsRevenue's
   * shim keeps it out of faturamento. Re-sending the blank field would be a
   * deliberate write, so an untouched origin must not appear in the patch at
   * all — otherwise fixing a typo in a description would relabel the entry.
   */
  it('leaves an untouched origin out of the patch', () => {
    const legacy = values({ origin: '', type: 'income' })
    const patch = editEntryPatch(legacy, { ...legacy, description: 'corrigido' })
    expect(patch).toEqual({ description: 'corrigido' })
    expect('origin' in patch).toBe(false)
  })

  it('sends a corrected origin, which is what takes a loan out of faturamento', () => {
    const sale = values({ type: 'income', origin: 'venda' })
    expect(editEntryPatch(sale, { ...sale, origin: 'emprestimo' })).toEqual({
      origin: 'emprestimo',
    })
  })

  // Origin is meaningless on money going out: the server drops it, so sending
  // one alongside the type change would be noise.
  it('does not send an origin when the entry becomes an expense', () => {
    const income = values({ type: 'income', origin: 'venda' })
    const patch = editEntryPatch(income, { ...income, type: 'expense', origin: '' })
    expect(patch).toEqual({ type: 'expense' })
  })

  it('ignores an unusable amount rather than sending a broken one', () => {
    expect(isEmptyPatch(editEntryPatch(values(), values({ amount: 'abc' })))).toBe(true)
  })
})
