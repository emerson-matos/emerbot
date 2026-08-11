import { describe, expect, it } from 'vitest'
import {
  agingLabel,
  contaEstado,
  contaLegenda,
  devedorPath,
  formatDiaLabel,
  formatFiadoDate,
  movimentoLabels,
  movimentoTipo,
  saldoTexto,
} from './fiado'
import { makeDevedor, normalizeSpaces } from '@/test/factories'

describe('agingLabel', () => {
  it('conta os dias em aberto, que é o único jeito de falar da idade', () => {
    expect(agingLabel(60)).toBe('em aberto há 60 dias')
  })

  it('concorda no singular', () => {
    expect(agingLabel(1)).toBe('em aberto há 1 dia')
  })

  /**
   * Uma dívida de hoje não tem idade medível (ADR-017), então "há 0 dias" não
   * é uma frase — o dia ainda está acontecendo.
   */
  it('não diz "há 0 dias" para uma dívida que começou hoje', () => {
    expect(agingLabel(0)).toBe('começou hoje')
  })

  it('não inventa idade quando não há dívida aberta', () => {
    expect(agingLabel(null)).toBeNull()
    expect(agingLabel(undefined)).toBeNull()
  })

  /**
   * A decisão central da ADR-027: nada foi prometido, então nada está
   * atrasado. Este teste existe para o vocabulário não voltar por descuido.
   */
  it('nunca fala em vencimento, atraso ou inadimplência', () => {
    const frases = [30, 1, 0, 365]
      .map(dias => agingLabel(dias) ?? '')
      .concat(
        contaLegenda(makeDevedor()),
        contaLegenda(makeDevedor({ saldo: 0, desde: null, dias_em_aberto: null })),
        contaLegenda(makeDevedor({ saldo: -500, desde: null, dias_em_aberto: null })),
        Object.values(movimentoLabels),
      )

    for (const frase of frases) {
      expect(frase).not.toMatch(/venc|atras|inadimpl/i)
    }
  })
})

describe('contaEstado', () => {
  it('separa dívida, conta quitada e crédito do cliente', () => {
    expect(contaEstado(34000)).toBe('devendo')
    expect(contaEstado(0)).toBe('quite')
    expect(contaEstado(-500)).toBe('credito')
  })
})

describe('contaLegenda', () => {
  it('mostra a idade de quem está devendo', () => {
    expect(contaLegenda(makeDevedor({ saldo: 34000, dias_em_aberto: 60 }))).toBe(
      'em aberto há 60 dias',
    )
  })

  it('diz que a conta está limpa em vez de mostrar um zero mudo', () => {
    expect(
      contaLegenda(makeDevedor({ saldo: 0, desde: null, dias_em_aberto: null })),
    ).toBe('sem nada em aberto')
  })

  /**
   * Saldo negativo é o cliente que pagou mais do que devia. Registrar é melhor
   * que recusar, e ler isso como dívida seria cobrar quem tem crédito — que é
   * exatamente o caso em que o backend manda `dias_em_aberto: null`.
   */
  it('não dá idade a quem está em crédito', () => {
    expect(
      contaLegenda(makeDevedor({ saldo: -5000, desde: null, dias_em_aberto: null })),
    ).toBe('pagou mais do que devia')
  })

  it('não trava quando há dívida sem idade informada', () => {
    expect(contaLegenda(makeDevedor({ saldo: 1000, dias_em_aberto: null }))).toBe(
      'em aberto',
    )
  })
})

describe('saldoTexto', () => {
  it('mostra a dívida como valor', () => {
    expect(normalizeSpaces(saldoTexto(34000))).toBe('R$ 340,00')
  })

  it('mostra a conta quitada como zero, não como vazio', () => {
    expect(normalizeSpaces(saldoTexto(0))).toBe('R$ 0,00')
  })

  /**
   * Um crédito escrito "−R$ 50,00" no meio de uma lista de dívidas é lido como
   * desconto do total em aberto, e o total não é esse.
   */
  it('diz o crédito por extenso, com o valor no positivo', () => {
    expect(normalizeSpaces(saldoTexto(-5000))).toBe('R$ 50,00 a favor')
  })
})

describe('movimentoTipo', () => {
  // O sinal é o tipo: não há campo a consultar, então esta é a leitura.
  it('lê dívida no positivo e pagamento no negativo', () => {
    expect(movimentoTipo(4000)).toBe('divida')
    expect(movimentoTipo(-5000)).toBe('pagamento')
  })
})

describe('formatação de datas e endereços', () => {
  it('formata a data sem escorregar de dia por causa do fuso', () => {
    expect(formatFiadoDate('2026-06-12')).toBe('12/06/2026')
  })

  it('devolve o que recebeu quando a data não é uma data', () => {
    expect(formatFiadoDate('nem-data')).toBe('nem-data')
  })

  it('escreve o dia por extenso, em minúsculas, para o render capitalizar', () => {
    expect(formatDiaLabel('2026-08-10')).toBe('segunda-feira, 10 de agosto de 2026')
  })

  it('escapa o slug no endereço do devedor', () => {
    expect(devedorPath('joao_silva')).toBe('/fiado/joao_silva')
    expect(devedorPath('joão silva')).toBe('/fiado/jo%C3%A3o%20silva')
  })
})
