import { format, isValid, parseISO } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import type { Entry } from '../api/client'

// Tables are about *due* transactions: pending entries show when they're
// due, and already-settled ones (no DueDate) fall back to when they happened.
export function effectiveDate(e: Entry): string | null {
  return e.DueDate || e.Date
}

export function formatEffectiveDate(e: Entry): string {
  const iso = effectiveDate(e)
  if (!iso) return '—'
  const parsed = parseISO(iso)
  return isValid(parsed) ? format(parsed, 'dd/MM/yy', { locale: ptBR }) : '—'
}

export function formatPaidAt(e: Entry): string {
  if (!e.PaymentDate) return ''
  const parsed = parseISO(e.PaymentDate)
  return isValid(parsed) ? `em ${format(parsed, 'dd/MM', { locale: ptBR })}` : ''
}
