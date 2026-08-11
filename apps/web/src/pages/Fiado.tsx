import { useState } from 'react'
import { CalendarDays, NotebookPen, Users } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import KpiCard, { KpiCardContent, toneVar } from '@/components/KpiCard'
import EmptyState from '@/components/EmptyState'
import DevedorList from '@/components/fiado/DevedorList'
import MovimentoList from '@/components/fiado/MovimentoList'
import ListaCortadaAviso from '@/components/fiado/ListaCortadaAviso'
import { formatBRL } from '@/lib/format'
import { formatDiaLabel } from '@/lib/fiado'
import { todayISO } from '@/lib/entries'
import { useCaderninho, useFiadoMovimentosDoDia } from '../api/queries'

function ListaSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-11 rounded-md" />
      ))}
    </div>
  )
}

/**
 * O caderninho de fiado.
 *
 * Controle interno e nada além disso: o que está aqui não é faturamento, não é
 * caixa e não entra em métrica nenhuma (ADR-027). E como nada foi combinado com
 * ninguém, nada aqui está atrasado — uma conta envelhece, e é assim que a tela
 * fala dela.
 */
export default function Fiado() {
  // O dia começa em hoje, no fuso da farmácia. Esvaziar o campo não faz a
  // consulta cair de volta em hoje: "sem data" e "hoje" são respostas
  // diferentes, e a tela diz qual das duas está vendo.
  const [dia, setDia] = useState(() => todayISO())

  const caderninho = useCaderninho()
  const movimentosDoDia = useFiadoMovimentosDoDia(dia)

  const devedores = caderninho.data?.devedores ?? []
  const movimentos = movimentosDoDia.data?.movimentos ?? []

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Caderninho</h1>
        <p className="mt-1 text-muted-foreground">
          Quem levou fiado, quanto e desde quando. É controle interno: não entra
          em faturamento nem no caixa.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <KpiCard
          tone="warning"
          isLoading={caderninho.isLoading}
          isError={caderninho.isError}
          errorMessage="Erro ao carregar o caderninho"
          className="min-h-32"
        >
          <KpiCardContent icon={NotebookPen} tone="warning">
            <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
              Total em aberto
            </p>
            <p
              className="mt-1 text-2xl font-semibold tabular-nums"
              style={{ color: toneVar.warning }}
            >
              {formatBRL(caderninho.data?.total_em_aberto ?? 0)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              Soma do que está no caderninho
            </p>
          </KpiCardContent>
        </KpiCard>

        <KpiCard
          tone="info"
          isLoading={caderninho.isLoading}
          isError={caderninho.isError}
          errorMessage="Erro ao carregar o caderninho"
          className="min-h-32"
        >
          <KpiCardContent icon={Users} tone="info">
            <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
              No caderninho
            </p>
            <p
              className="mt-1 text-2xl font-semibold tabular-nums"
              style={{ color: toneVar.info }}
            >
              {caderninho.data?.count ?? 0}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {caderninho.data?.count === 1 ? 'pessoa' : 'pessoas'} com conta
            </p>
          </KpiCardContent>
        </KpiCard>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="flex items-center gap-2 text-sm">
            <NotebookPen className="size-4 text-primary" aria-hidden />
            Quem está devendo
          </CardTitle>
        </CardHeader>
        <CardContent>
          {caderninho.isLoading ? (
            <ListaSkeleton />
          ) : caderninho.isError ? (
            <p className="py-6 text-center text-sm text-destructive">
              Erro ao carregar o caderninho.
            </p>
          ) : devedores.length === 0 ? (
            // Ninguém devendo é uma resposta boa, não uma falta de dado.
            <EmptyState
              icon={NotebookPen}
              message="Ninguém está devendo. O caderninho está limpo."
            />
          ) : (
            <DevedorList devedores={devedores} />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span className="flex items-center gap-2">
              <CalendarDays className="size-4 text-primary" aria-hidden />
              Movimento do dia
            </span>
            <Input
              type="date"
              value={dia}
              onChange={event => setDia(event.target.value)}
              aria-label="Dia do caderninho"
              className="h-8 w-auto font-normal tabular-nums"
            />
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {dia !== '' && (
            <p className="text-xs text-muted-foreground first-letter:uppercase">
              {formatDiaLabel(dia)}
            </p>
          )}

          {dia === '' ? (
            <p className="py-6 text-center text-sm text-muted-foreground">
              Escolha uma data para ver o movimento do dia.
            </p>
          ) : movimentosDoDia.isLoading ? (
            <ListaSkeleton rows={3} />
          ) : movimentosDoDia.isError ? (
            <p className="py-6 text-center text-sm text-destructive">
              Erro ao carregar o movimento do dia.
            </p>
          ) : movimentos.length === 0 ? (
            <EmptyState
              icon={CalendarDays}
              message="Nenhum movimento de fiado nesta data."
            />
          ) : (
            <>
              <ListaCortadaAviso warning={movimentosDoDia.data?.warning} />
              <MovimentoList movimentos={movimentos} mode="dia" />
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
