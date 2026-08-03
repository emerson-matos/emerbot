import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import type { Entry } from '@/api/types'
import { makeCategory, makeEntry } from '@/test/factories'

const useEntry = vi.hoisted(() => vi.fn())
const useCategories = vi.hoisted(() => vi.fn())
const mutateAsync = vi.hoisted(() => vi.fn())
const useUpdateEntryMutation = vi.hoisted(() => vi.fn())

vi.mock('../api/queries', () => ({ useEntry, useCategories, useUpdateEntryMutation }))

import EditarTransacao from './EditarTransacao'

function renderPage(entryQuery: Record<string, unknown>) {
  useEntry.mockReturnValue(entryQuery)
  useCategories.mockReturnValue({
    data: [
      makeCategory({ Slug: 'aluguel', Label: 'Aluguel', Type: 'expense' }),
      makeCategory({ Slug: 'venda_balcao', Label: 'Venda Balcão', Type: 'income' }),
    ],
  })
  useUpdateEntryMutation.mockReturnValue({ mutateAsync, isPending: false })

  return render(
    <MemoryRouter initialEntries={['/transacoes/e1/editar']}>
      <Routes>
        <Route path="/transacoes/:id/editar" element={<EditarTransacao />} />
        <Route path="/transacoes" element={<p>lista de transações</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

function loaded(overrides: Partial<Entry> = {}) {
  return {
    data: makeEntry({
      EntryID: 'e1',
      Type: 'expense',
      Category: 'aluguel',
      Description: 'Aluguel da loja',
      Amount: 250000,
      TransactionDate: '2026-07-10',
      PaymentStatus: 'pending',
      ...overrides,
    }),
    isLoading: false,
    isError: false,
  }
}

const description = () => screen.getByLabelText('Descrição')

describe('EditarTransacao', () => {
  beforeEach(() => {
    mutateAsync.mockReset()
    mutateAsync.mockResolvedValue(makeEntry())
  })

  it('prefills the form from the stored entry', () => {
    renderPage(loaded())

    expect(description()).toHaveValue('Aluguel da loja')
    expect(screen.getByLabelText('Valor (R$)')).toHaveValue(2500)
    expect(screen.getByLabelText('Data da transação')).toHaveValue('2026-07-10')
  })

  /**
   * The API reads a present key as an instruction, so sending the whole form
   * back would be an edit of every field — including the origin of an entry
   * written before that field existed.
   */
  it('sends only the field that was changed', async () => {
    renderPage(loaded())

    fireEvent.change(description(), { target: { value: 'Aluguel da loja — julho' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar alterações' }))

    expect(mutateAsync).toHaveBeenCalledWith({ description: 'Aluguel da loja — julho' })
  })

  it('sends the corrected date, the commonest reason to edit at all', () => {
    renderPage(loaded())

    fireEvent.change(screen.getByLabelText('Data da transação'), {
      target: { value: '2026-08-03' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar alterações' }))

    expect(mutateAsync).toHaveBeenCalledWith({ date: '2026-08-03' })
  })

  it('clears an emptied due date instead of leaving it in place', () => {
    renderPage(loaded({ DueDate: '2026-07-20' }))

    fireEvent.change(screen.getByLabelText('Vencimento'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar alterações' }))

    expect(mutateAsync).toHaveBeenCalledWith({ due_date: '' })
  })

  it('refuses to save an entry with no description', () => {
    renderPage(loaded())

    fireEvent.change(description(), { target: { value: '  ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar alterações' }))

    expect(mutateAsync).not.toHaveBeenCalled()
    expect(screen.getByText('Campo obrigatório')).toBeInTheDocument()
  })

  it('has nothing to save until something changes', () => {
    renderPage(loaded())

    expect(screen.getByRole('button', { name: 'Salvar alterações' })).toBeDisabled()
    expect(screen.getByText('Nada alterado')).toBeInTheDocument()
  })

  // Editing one instalment of a /recorrente series leaves the others alone, so
  // the page has to say so rather than let the user assume a bulk edit.
  it('says which instalment of a series is being edited', () => {
    renderPage(loaded({ RecurrenceID: 'serie-1', RecurrenceIndex: 2, RecurrenceTotal: 6 }))

    expect(screen.getByText(/Parcela 2 de 6/)).toBeInTheDocument()
    expect(screen.getByText(/vale só para esta parcela/)).toBeInTheDocument()
  })

  it('does not mention a series for a one-off entry', () => {
    renderPage(loaded())

    expect(screen.queryByText(/série recorrente/)).not.toBeInTheDocument()
  })

  it('says so when the entry does not exist', () => {
    renderPage({ data: undefined, isLoading: false, isError: true })

    expect(screen.getByText('Lançamento não encontrado.')).toBeInTheDocument()
  })
})
