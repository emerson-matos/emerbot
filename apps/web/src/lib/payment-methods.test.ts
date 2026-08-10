import { beforeEach, describe, expect, it } from 'vitest'
import {
  commonPaymentMethods,
  lastPaymentMethod,
  paymentMethodSuggestions,
  recentPaymentMethods,
  rememberPaymentMethod,
} from './payment-methods'

describe('payment method suggestions', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('offers the common forms before anything has been used', () => {
    expect(recentPaymentMethods()).toEqual([])
    expect(paymentMethodSuggestions()).toEqual(commonPaymentMethods)
    // Nothing pre-filled on the very first quitação: empty is a fine answer.
    expect(lastPaymentMethod()).toBe('')
  })

  it('puts what was used last at the top', () => {
    rememberPaymentMethod('Boleto')
    rememberPaymentMethod('Pix')

    expect(lastPaymentMethod()).toBe('Pix')
    expect(paymentMethodSuggestions().slice(0, 2)).toEqual(['Pix', 'Boleto'])
  })

  // The whole point of the list: keeping "pix", "PIX" and "Pix " from becoming
  // three answers to the same question.
  it('folds spellings of the same form together, keeping the newest', () => {
    rememberPaymentMethod('Pix')
    rememberPaymentMethod('  pix ')

    expect(recentPaymentMethods()).toEqual(['pix'])
    expect(paymentMethodSuggestions().filter(m => m.toLowerCase() === 'pix')).toEqual(['pix'])
  })

  it('ignores a blank form rather than remembering nothing', () => {
    rememberPaymentMethod('   ')
    expect(recentPaymentMethods()).toEqual([])
  })

  it('keeps the recent list short', () => {
    for (const m of ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h']) rememberPaymentMethod(m)
    expect(recentPaymentMethods()).toEqual(['h', 'g', 'f', 'e', 'd', 'c'])
  })

  // A suggestion list is never worth breaking the page it decorates.
  it('survives corrupted storage', () => {
    window.localStorage.setItem('emerbot.payment-methods.recent', '{not json')
    expect(recentPaymentMethods()).toEqual([])
    expect(paymentMethodSuggestions()).toEqual(commonPaymentMethods)
  })
})
