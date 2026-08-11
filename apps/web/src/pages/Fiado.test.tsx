import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { makeDevedor, makeMovimento, normalizeSpaces } from '@/test/factories'

const useCaderninho = vi.hoisted(() => vi.fn())
const useFiadoMovimentosDoDia = vi.hoisted(() => vi.fn())

vi.mock('../api/queries', () => ({ useCaderninho, useFiadoMovimentosDoDia }))

import Fiado from './Fiado'

function caderninho(overrides: Record<string, unknown> = {}) {
  return {
    data: {
      devedores: [
        makeDevedor(),
        makeDevedor({
          cliente: 'maria',
          nome: 'Maria',
          saldo: 18000,
          desde: '2026-08-07',
          dias_em_aberto: 3,
        }),
      ],
      total_em_aberto: 52000,
      count: 2,
    },
    isLoading: false,
    isError: false,
    ...overrides,
  }
}

function movimentosDoDia(overrides: Record<string, unknown> = {}) {
  return {
    data: { movimentos: [], count: 0, truncated: false },
    isLoading: false,
    isError: false,
    ...overrides,
  }
}

function renderPage(
  caderninhoQuery = caderninho(),
  diaQuery = movimentosDoDia(),
) {
  useCaderninho.mockReturnValue(caderninhoQuery)
  useFiadoMovimentosDoDia.mockReturnValue(diaQuery)

  return render(
    <MemoryRouter>
      <Fiado />
    </MemoryRouter>,
  )
}

describe('Fiado', () => {
  beforeEach(() => {
    useCaderninho.mockReset()
    useFiadoMovimentosDoDia.mockReset()
  })

  it('mostra o total em aberto e quantas pessoas estão no caderninho', () => {
    renderPage()

    expect(normalizeSpaces(screen.getByText(/R\$\s?520,00/).textContent!)).toBe(
      'R$ 520,00',
    )
    expect(screen.getByText('No caderninho')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.getByText(/pessoas com conta/)).toBeInTheDocument()
  })

  it('lista o devedor com o nome, o saldo e há quantos dias está em aberto', () => {
    renderPage()

    const link = screen.getByRole('link', { name: /João Silva/ })
    expect(link).toHaveAttribute('href', '/fiado/joao_silva')
    expect(within(link).getByText('em aberto há 60 dias')).toBeInTheDocument()
  })

  /** Ninguém devendo é uma resposta boa — não um erro, e não uma tela vazia. */
  it('diz que o caderninho está limpo quando ninguém deve', () => {
    renderPage(
      caderninho({ data: { devedores: [], total_em_aberto: 0, count: 0 } }),
    )

    expect(
      screen.getByText('Ninguém está devendo. O caderninho está limpo.'),
    ).toBeInTheDocument()
    expect(screen.queryByText(/Erro ao carregar/)).not.toBeInTheDocument()
  })

  /**
   * Saldo negativo é crédito do cliente, e nesse caso `dias_em_aberto` vem
   * null: quem tem crédito não está devendo há dia nenhum.
   */
  it('mostra crédito a favor sem dar idade a quem não está devendo', () => {
    renderPage(
      caderninho({
        data: {
          devedores: [
            makeDevedor({
              cliente: 'ana',
              nome: 'Ana',
              saldo: -5000,
              desde: null,
              dias_em_aberto: null,
            }),
          ],
          total_em_aberto: 0,
          count: 1,
        },
      }),
    )

    const link = screen.getByRole('link', { name: /Ana/ })
    expect(normalizeSpaces(link.textContent ?? '')).toContain('R$ 50,00 a favor')
    expect(within(link).getByText('pagou mais do que devia')).toBeInTheDocument()
    expect(link.textContent).not.toMatch(/em aberto há/)
  })

  it('avisa quando a leitura do caderninho falha, em vez de mostrar zero', () => {
    renderPage(caderninho({ data: undefined, isError: true }))

    expect(screen.getByText('Erro ao carregar o caderninho.')).toBeInTheDocument()
  })

  it('mostra o movimento do dia com o sinal de cada linha', () => {
    renderPage(
      caderninho(),
      movimentosDoDia({
        data: {
          movimentos: [
            makeMovimento({ id: 'm1', valor: 4000, descricao: 'dipirona' }),
            makeMovimento({
              id: 'm2',
              nome: 'Maria',
              cliente: 'maria',
              valor: -5000,
              descricao: 'pagou em dinheiro',
            }),
          ],
          count: 2,
          truncated: false,
        },
      }),
    )

    expect(
      normalizeSpaces(screen.getByText(/\+\s?R\$\s?40,00/).textContent!),
    ).toBe('+ R$ 40,00')
    expect(
      normalizeSpaces(screen.getByText(/−\s?R\$\s?50,00/).textContent!),
    ).toBe('− R$ 50,00')
    expect(screen.getByText(/levou fiado · dipirona/)).toBeInTheDocument()
    expect(screen.getByText(/pagou · pagou em dinheiro/)).toBeInTheDocument()
  })

  /** ADR-015: uma lista cortada nunca sai calada. */
  it('mostra o aviso quando a lista do dia veio cortada', () => {
    renderPage(
      caderninho(),
      movimentosDoDia({
        data: {
          movimentos: [makeMovimento()],
          count: 1,
          truncated: true,
          warning: 'Mostrando 200 de mais movimentos neste dia.',
        },
      }),
    )

    expect(
      screen.getByText('Mostrando 200 de mais movimentos neste dia.'),
    ).toBeInTheDocument()
  })

  it('diz que não houve movimento no dia sem chamar isso de erro', () => {
    renderPage()

    expect(
      screen.getByText('Nenhum movimento de fiado nesta data.'),
    ).toBeInTheDocument()
  })

  /**
   * Uma data apagada não volta a ser hoje em silêncio: "sem data" e "hoje" são
   * respostas diferentes e a tela tem de dizer qual está mostrando.
   */
  it('pede uma data em vez de assumir hoje quando o campo é esvaziado', () => {
    renderPage()

    fireEvent.change(screen.getByLabelText('Dia do caderninho'), {
      target: { value: '' },
    })

    expect(
      screen.getByText('Escolha uma data para ver o movimento do dia.'),
    ).toBeInTheDocument()
  })
})
