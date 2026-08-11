import { Link } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { contaEstado, contaLegenda, devedorPath, saldoTexto } from '@/lib/fiado'
import type { Devedor } from '@/api/types'

const saldoTone: Record<ReturnType<typeof contaEstado>, string> = {
  devendo: 'text-warning',
  credito: 'text-success',
  quite: 'text-muted-foreground',
}

/**
 * Quem está devendo, na ordem que o backend mandou (saldo desc) — reordenar
 * aqui só faria a tela discordar do total que está em cima dela.
 *
 * Cada linha leva ao extrato da pessoa: o saldo responde "quanto", e o "desde
 * quando" está na legenda; o resto é a timeline dela.
 */
export default function DevedorList({ devedores }: { devedores: Devedor[] }) {
  return (
    <ul className="divide-y divide-border">
      {devedores.map(devedor => (
        <li key={devedor.cliente}>
          <Link
            to={devedorPath(devedor.cliente)}
            className="-mx-2 grid grid-cols-[1fr_auto_auto] items-center gap-3 rounded-lg px-2 py-3 transition-colors hover:bg-muted/50"
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{devedor.nome}</p>
              <p className="mt-0.5 truncate text-xs text-muted-foreground">
                {contaLegenda(devedor)}
              </p>
            </div>
            <span
              className={cn(
                'text-sm font-semibold tabular-nums',
                saldoTone[contaEstado(devedor.saldo)],
              )}
            >
              {saldoTexto(devedor.saldo)}
            </span>
            <ChevronRight
              aria-hidden
              className="size-4 shrink-0 text-muted-foreground"
            />
          </Link>
        </li>
      ))}
    </ul>
  )
}
