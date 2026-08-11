import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, ChevronDown, NotebookPen } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import EmptyState from '@/components/EmptyState'
import MovimentoList from '@/components/fiado/MovimentoList'
import ListaCortadaAviso from '@/components/fiado/ListaCortadaAviso'
import { cn } from '@/lib/utils'
import {
  contaEstado,
  contaLegenda,
  formatFiadoDate,
  saldoTexto,
} from '@/lib/fiado'
import { ApiError } from '@/api/api-error'
import { useDevedor, useFiadoMovimentos } from '../api/queries'

const saldoTone: Record<ReturnType<typeof contaEstado>, string> = {
  devendo: 'text-warning',
  credito: 'text-success',
  quite: 'text-muted-foreground',
}

function VoltarAoCaderninho() {
  return (
    <Button
      variant="ghost"
      size="sm"
      className="-ml-2 text-muted-foreground"
      render={<Link to="/fiado" />}
      nativeButton={false}
    >
      <ArrowLeft data-icon="inline-start" /> Caderninho
    </Button>
  )
}

/**
 * O extrato de uma pessoa: o saldo dela em cima, e embaixo o que aconteceu.
 *
 * O saldo aparece uma vez só. A timeline não tem coluna de saldo acumulado —
 * numa lista paginada ela começaria no meio da história, e ninguém pediu por
 * ela. Cada linha diz o seu sinal, que é o que separa o que a pessoa levou do
 * que ela pagou.
 */
export default function FiadoCliente() {
  const { cliente } = useParams<{ cliente: string }>()

  const devedorQuery = useDevedor(cliente)
  const movimentosQuery = useFiadoMovimentos(cliente)

  const devedor = devedorQuery.data
  const paginas = movimentosQuery.data?.pages ?? []
  const movimentos = paginas.flatMap(pagina => pagina.movimentos)
  // O aviso é o da última página carregada: o da primeira deixa de valer assim
  // que se carrega mais uma, e repeti-lo diria que ainda falta algo já lido.
  const ultimaPagina = paginas.at(-1)

  const naoEncontrado =
    devedorQuery.error instanceof ApiError && devedorQuery.error.status === 404

  if (naoEncontrado) {
    return (
      <div className="space-y-4">
        <VoltarAoCaderninho />
        <Card>
          <CardContent>
            <EmptyState
              icon={NotebookPen}
              message="Essa pessoa não está no caderninho."
            />
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <VoltarAoCaderninho />
        <div>
          <h1 className="text-3xl font-semibold tracking-tight">
            {devedor?.nome ?? cliente}
          </h1>
          <p className="mt-1 text-muted-foreground">
            O que essa pessoa levou fiado e o que já pagou.
          </p>
        </div>
      </div>

      <Card>
        <CardContent>
          {devedorQuery.isLoading ? (
            <Skeleton className="h-14 rounded-md" />
          ) : devedorQuery.isError || !devedor ? (
            <p className="text-sm text-destructive">
              Erro ao carregar o saldo dessa pessoa.
            </p>
          ) : (
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div>
                <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                  Saldo
                </p>
                <p
                  className={cn(
                    'mt-1 text-3xl font-semibold tabular-nums',
                    saldoTone[contaEstado(devedor.saldo)],
                  )}
                >
                  {saldoTexto(devedor.saldo)}
                </p>
              </div>
              <p className="text-sm text-muted-foreground">
                {contaLegenda(devedor)}
                {devedor.desde ? ` · desde ${formatFiadoDate(devedor.desde)}` : ''}
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Movimentos</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {movimentosQuery.isLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-11 rounded-md" />
              ))}
            </div>
          ) : movimentosQuery.isError ? (
            <p className="py-6 text-center text-sm text-destructive">
              Erro ao carregar os movimentos.
            </p>
          ) : movimentos.length === 0 ? (
            <EmptyState
              icon={NotebookPen}
              message="Nenhum movimento registrado para essa pessoa."
            />
          ) : (
            <>
              <ListaCortadaAviso warning={ultimaPagina?.warning} />
              <MovimentoList movimentos={movimentos} mode="cliente" />
              {movimentosQuery.hasNextPage && (
                <div className="flex justify-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={movimentosQuery.isFetchingNextPage}
                    onClick={() => movimentosQuery.fetchNextPage()}
                  >
                    <ChevronDown className="size-3.5" />
                    {movimentosQuery.isFetchingNextPage
                      ? 'Carregando...'
                      : 'Carregar mais'}
                  </Button>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
