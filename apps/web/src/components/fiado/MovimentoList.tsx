import { ArrowDownLeft, ArrowUpRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { formatSignedBRL } from '@/lib/format'
import { formatFiadoDate, movimentoLabels, movimentoTipo } from '@/lib/fiado'
import type { FiadoMovimento } from '@/api/types'

interface Props {
  movimentos: FiadoMovimento[]
  /**
   * Qual pergunta a lista responde. No extrato de uma pessoa o nome dela é
   * redundante e o assunto é a descrição; no dia é o contrário — quem levou (ou
   * pagou) é justamente o que se quer ler.
   */
  mode: 'cliente' | 'dia'
}

/**
 * A timeline do caderninho: o que aconteceu, na ordem em que aconteceu, do
 * mais recente para o mais antigo.
 *
 * Não há coluna de saldo, e não deve haver: o saldo é o do devedor e aparece
 * uma vez, no topo da página. O que cada linha carrega é o sinal — `+` é
 * dívida, `−` é pagamento — e ele vai no texto além da cor, porque cor sozinha
 * não é informação para quem não a distingue.
 */
export default function MovimentoList({ movimentos, mode }: Props) {
  return (
    <ul className="divide-y divide-border">
      {movimentos.map(movimento => {
        const tipo = movimentoTipo(movimento.valor)
        const pagamento = tipo === 'pagamento'
        const titulo =
          mode === 'dia'
            ? movimento.nome
            : movimento.descricao || movimentoLabels[tipo]
        // No dia, a data é a mesma para todas as linhas e repeti-la é ruído; o
        // que falta ali é o que a linha significa.
        const detalhe =
          mode === 'dia'
            ? [movimentoLabels[tipo], movimento.descricao]
                .filter(Boolean)
                .join(' · ')
            : `${formatFiadoDate(movimento.data)} · ${movimentoLabels[tipo]}`

        return (
          <li
            key={movimento.id}
            className="grid grid-cols-[auto_1fr_auto] items-center gap-3 py-3"
          >
            <span
              aria-hidden
              className={cn(
                'grid size-8 shrink-0 place-items-center rounded-lg',
                pagamento
                  ? 'bg-success/10 text-success'
                  : 'bg-warning/10 text-warning',
              )}
            >
              {pagamento ? (
                <ArrowDownLeft className="size-4" />
              ) : (
                <ArrowUpRight className="size-4" />
              )}
            </span>

            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{titulo}</p>
              <p className="mt-0.5 truncate text-xs text-muted-foreground">
                {detalhe}
              </p>
            </div>

            <span
              className={cn(
                'text-sm font-semibold tabular-nums',
                pagamento ? 'text-success' : 'text-warning',
              )}
            >
              {formatSignedBRL(movimento.valor)}
            </span>
          </li>
        )
      })}
    </ul>
  )
}
