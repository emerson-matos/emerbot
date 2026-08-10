import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { makeEntry } from '@/test/factories'

const useCategories = vi.hoisted(() => vi.fn())
vi.mock('@/api/queries', () => ({ useCategories }))

import PaymentCard from './PaymentCard'

function renderCard(onMarkPaid: (entry: unknown, method: string) => void) {
  useCategories.mockReturnValue({ data: [] })
  return render(
    <MemoryRouter>
      <PaymentCard
        entry={makeEntry({
          EntryID: 'e1',
          Type: 'expense',
          Description: 'Aluguel da loja',
          Amount: 250000,
          TransactionDate: '2026-07-10',
          PaymentStatus: 'pending',
        })}
        onMarkPaid={onMarkPaid}
      />
    </MemoryRouter>,
  )
}

const methodField = () => screen.getByLabelText(/Forma de pagamento/)

describe('quitar um lançamento', () => {
  beforeEach(() => {
    window.localStorage.clear()
    useCategories.mockClear()
  })

  it('records the form of payment that was typed', () => {
    const onMarkPaid = vi.fn()
    renderCard(onMarkPaid)

    fireEvent.click(screen.getByRole('button', { name: /Pagar/ }))
    fireEvent.change(methodField(), { target: { value: 'pix' } })
    fireEvent.click(screen.getByRole('button', { name: 'Confirmar' }))

    expect(onMarkPaid).toHaveBeenCalledTimes(1)
    expect(onMarkPaid.mock.calls[0][1]).toBe('pix')
  })

  // The field is optional, and confirming without it must stay a single click —
  // "não informado" is the ordinary state of this field (ADR-026).
  it('quits the entry with no form of payment at all', () => {
    const onMarkPaid = vi.fn()
    renderCard(onMarkPaid)

    fireEvent.click(screen.getByRole('button', { name: /Pagar/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirmar' }))

    expect(onMarkPaid.mock.calls[0][1]).toBe('')
  })

  it('offers the last form used as the default on the next quitação', () => {
    const onMarkPaid = vi.fn()
    const first = renderCard(onMarkPaid)

    fireEvent.click(screen.getByRole('button', { name: /Pagar/ }))
    fireEvent.change(methodField(), { target: { value: 'Boleto' } })
    fireEvent.click(screen.getByRole('button', { name: 'Confirmar' }))
    first.unmount()

    renderCard(onMarkPaid)
    fireEvent.click(screen.getByRole('button', { name: /Pagar/ }))
    expect(methodField()).toHaveValue('Boleto')
  })

  it('shows how a settled entry was paid', () => {
    useCategories.mockReturnValue({ data: [] })
    render(
      <MemoryRouter>
        <PaymentCard
          entry={makeEntry({
            EntryID: 'e2',
            Type: 'expense',
            Description: 'Energia',
            PaymentStatus: 'paid',
            PaymentDate: '2026-07-12',
            PaymentMethod: 'pix',
          })}
        />
      </MemoryRouter>,
    )

    expect(screen.getByText(/pago em 12\/07 · pix/)).toBeInTheDocument()
  })
})
