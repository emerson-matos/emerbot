import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/api-error'
import { makeDevedor, makeMovimento, normalizeSpaces } from '@/test/factories'

const useDevedor = vi.hoisted(() => vi.fn())
const useFiadoMovimentos = vi.hoisted(() => vi.fn())
const fetchNextPage = vi.hoisted(() => vi.fn())

vi.mock('../api/queries', () => ({ useDevedor, useFiadoMovimentos }))

import FiadoCliente from './FiadoCliente'

function devedorQuery(overrides: Record<string, unknown> = {}) {
  return {
    data: makeDevedor(),
    isLoading: false,
    isError: false,
    error: null,
    ...overrides,
  }
}

function movimentosQuery(overrides: Record<string, unknown> = {}) {
  return {
    data: {
      pages: [
        {
          movimentos: [
            makeMovimento({ id: 'm1', valor: 4000, descricao: 'dipirona' }),
            makeMovimento({
              id: 'm2',
              valor: -5000,
              data: '2026-08-09',
              descricao: 'pagou em dinheiro',
            }),
          ],
          count: 2,
          truncated: false,
        },
      ],
    },
    isLoading: false,
    isError: false,
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage,
    ...overrides,
  }
}

function renderPage(devedor = devedorQuery(), movimentos = movimentosQuery()) {
  useDevedor.mockReturnValue(devedor)
  useFiadoMovimentos.mockReturnValue(movimentos)

  return render(
    <MemoryRouter initialEntries={['/fiado/joao_silva']}>
      <Routes>
        <Route path="/fiado/:cliente" element={<FiadoCliente />} />
        <Route path="/fiado" element={<p>caderninho</p>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('FiadoCliente', () => {
  beforeEach(() => {
    useDevedor.mockReset()
    useFiadoMovimentos.mockReset()
    fetchNextPage.mockReset()
  })

  it('carrega a pessoa pelo slug da URL', () => {
    renderPage()

    expect(useDevedor).toHaveBeenCalledWith('joao_silva')
    expect(useFiadoMovimentos).toHaveBeenCalledWith('joao_silva')
  })

  it('mostra o saldo uma vez, no topo, com desde quando está em aberto', () => {
    renderPage()

    expect(normalizeSpaces(screen.getByText(/R\$\s?340,00/).textContent!)).toBe(
      'R$ 340,00',
    )
    expect(
      screen.getByText(/em aberto há 60 dias · desde 12\/06\/2026/),
    ).toBeInTheDocument()
  })

  it('escreve o sinal de cada movimento: positivo levou, negativo pagou', () => {
    renderPage()

    expect(
      normalizeSpaces(screen.getByText(/\+\s?R\$\s?40,00/).textContent!),
    ).toBe('+ R$ 40,00')
    expect(
      normalizeSpaces(screen.getByText(/−\s?R\$\s?50,00/).textContent!),
    ).toBe('− R$ 50,00')
    expect(screen.getByText('10/08/2026 · levou fiado')).toBeInTheDocument()
    expect(screen.getByText('09/08/2026 · pagou')).toBeInTheDocument()
  })

  /**
   * Não há saldo por linha, e não é esquecimento: numa lista paginada um saldo
   * acumulado começaria no meio da história. Os únicos valores na tela são o
   * saldo do topo e os dois movimentos.
   */
  it('não mostra saldo acumulado por linha', () => {
    renderPage()

    const valores = screen
      .getAllByText(/R\$/)
      .map(node => normalizeSpaces(node.textContent ?? ''))

    expect(valores).toEqual(['R$ 340,00', '+ R$ 40,00', '− R$ 50,00'])
  })

  it('usa a descrição como título e cai no tipo do movimento quando não há', () => {
    renderPage(
      devedorQuery(),
      movimentosQuery({
        data: {
          pages: [
            {
              movimentos: [makeMovimento({ id: 'm1', descricao: '' })],
              count: 1,
              truncated: false,
            },
          ],
        },
      }),
    )

    expect(screen.getByText('levou fiado')).toBeInTheDocument()
  })

  it('carrega mais movimentos com o cursor da própria API', () => {
    renderPage(devedorQuery(), movimentosQuery({ hasNextPage: true }))

    fireEvent.click(screen.getByRole('button', { name: 'Carregar mais' }))

    expect(fetchNextPage).toHaveBeenCalled()
  })

  it('não oferece carregar mais quando a última página não tem cursor', () => {
    renderPage()

    expect(
      screen.queryByRole('button', { name: 'Carregar mais' }),
    ).not.toBeInTheDocument()
  })

  /**
   * O aviso é o da última página: o da primeira deixa de valer assim que se
   * carrega a próxima, e mantê-lo diria que ainda falta o que já foi lido.
   */
  it('mostra o aviso da última página carregada quando a lista foi cortada', () => {
    renderPage(
      devedorQuery(),
      movimentosQuery({
        data: {
          pages: [
            {
              movimentos: [makeMovimento({ id: 'm1' })],
              count: 1,
              truncated: true,
              warning: 'primeira página cortada',
            },
            {
              movimentos: [makeMovimento({ id: 'm2' })],
              count: 1,
              truncated: true,
              warning: 'ainda há mais movimentos do que cabe nesta lista.',
            },
          ],
        },
      }),
    )

    expect(
      screen.getByText('ainda há mais movimentos do que cabe nesta lista.'),
    ).toBeInTheDocument()
    expect(screen.queryByText('primeira página cortada')).not.toBeInTheDocument()
  })

  it('não avisa nada quando a lista veio inteira', () => {
    renderPage()

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('diz que a pessoa não está no caderninho quando a API responde 404', () => {
    renderPage(
      devedorQuery({
        data: undefined,
        isError: true,
        error: new ApiError(404),
      }),
    )

    expect(
      screen.getByText('Essa pessoa não está no caderninho.'),
    ).toBeInTheDocument()
  })

  /** Uma falha de leitura não é "essa pessoa não existe". */
  it('separa uma falha do servidor de uma pessoa inexistente', () => {
    renderPage(
      devedorQuery({
        data: undefined,
        isError: true,
        error: new ApiError(500),
      }),
    )

    expect(
      screen.getByText('Erro ao carregar o saldo dessa pessoa.'),
    ).toBeInTheDocument()
    expect(
      screen.queryByText('Essa pessoa não está no caderninho.'),
    ).not.toBeInTheDocument()
  })

  it('mostra crédito a favor sem inventar idade para ele', () => {
    renderPage(
      devedorQuery({
        data: makeDevedor({ saldo: -5000, desde: null, dias_em_aberto: null }),
      }),
    )

    expect(
      normalizeSpaces(screen.getByText(/a favor/).textContent!),
    ).toBe('R$ 50,00 a favor')
    expect(screen.getByText('pagou mais do que devia')).toBeInTheDocument()
    expect(screen.queryByText(/em aberto há/)).not.toBeInTheDocument()
    expect(screen.queryByText(/desde/)).not.toBeInTheDocument()
  })

  it('mostra a conta quitada sem chamar de dívida', () => {
    renderPage(
      devedorQuery({
        data: makeDevedor({ saldo: 0, desde: null, dias_em_aberto: null }),
      }),
    )

    expect(screen.getByText('sem nada em aberto')).toBeInTheDocument()
  })
})
