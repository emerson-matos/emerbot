import { TriangleAlert } from 'lucide-react'

/**
 * O aviso de que a lista veio cortada (ADR-015).
 *
 * Uma lista truncada renderizada em silêncio é pior que uma lista vazia: ela
 * parece completa. O texto é o do backend — quem cortou é quem sabe dizer o
 * quanto — e este componente só existe quando há o que avisar.
 */
export default function ListaCortadaAviso({ warning }: { warning?: string }) {
  if (!warning) return null

  return (
    <p
      role="status"
      className="flex items-start gap-2 rounded-lg bg-warning/10 px-3 py-2 text-xs text-warning"
    >
      <TriangleAlert className="mt-px size-3.5 shrink-0" aria-hidden />
      {warning}
    </p>
  )
}
