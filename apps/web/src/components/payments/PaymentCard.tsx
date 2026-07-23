import { format } from 'date-fns'
import { ArrowDownRight, ArrowUpRight, Check, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogFooter,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogCancel,
  AlertDialogAction,
} from '@/components/ui/alert-dialog'
import { cn } from '@/lib/utils'
import { formatBRL } from '@/lib/format'
import { categoryLabelMap } from '@/lib/categories'
import { effectiveDate, formatEffectiveDate, formatPaidAt } from '@/lib/entries'
import { useCategories } from '@/api/queries'
import type { Entry } from '@/api/types'

interface Props {
  entry: Entry
  onMarkPaid?: (id: string) => void
  onDelete?: (id: string) => void
}

export default function PaymentCard({ entry, onMarkPaid, onDelete }: Props) {
  const isIncome = entry.Type === 'income'
  const todayISO = format(new Date(), 'yyyy-MM-dd')
  const isOverdue = entry.PaymentStatus === 'pending' && (effectiveDate(entry) ?? '') < todayISO
  const categoriesQuery = useCategories()
  const categoryLabels = categoryLabelMap(categoriesQuery.data ?? [])

  return (
    <div className="grid grid-cols-[1fr_auto_auto] items-center gap-3 py-3.5 sm:grid-cols-[1fr_116px_120px] sm:gap-4">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{entry.Description || '—'}</p>
        <p className={cn('mt-0.5 truncate text-xs', isOverdue ? 'text-destructive' : 'text-muted-foreground')}>
          {formatEffectiveDate(entry)} · {categoryLabels[entry.Category] ?? entry.Category}
          {isOverdue && ' · em atraso'}
        </p>
      </div>

      <div className="flex justify-start">
        {entry.PaymentStatus === 'paid' ? (
          <span className="text-xs text-muted-foreground">
            {isIncome ? 'recebido' : 'pago'} {formatPaidAt(entry)}
          </span>
        ) : onMarkPaid ? (
          <AlertDialog>
            <AlertDialogTrigger
              render={
                <Button variant="outline" size="sm" className="h-8 w-22 text-xs">
                  <Check className="size-3.5" /> {isIncome ? 'Receber' : 'Pagar'}
                </Button>
              }
            />
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{isIncome ? 'Marcar como recebida?' : 'Marcar como paga?'}</AlertDialogTitle>
                <AlertDialogDescription>
                  "{entry.Description || 'Esta transação'}" será marcada como {isIncome ? 'recebida' : 'paga'}.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancelar</AlertDialogCancel>
                <AlertDialogAction onClick={() => onMarkPaid(entry.EntryID)}>
                  Confirmar
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        ) : null}
      </div>

      <div className="flex items-center justify-end gap-1.5">
        <span
          className={cn(
            'inline-flex items-center gap-1 text-sm font-semibold tabular-nums',
            isIncome ? 'text-success' : 'text-destructive',
          )}
        >
          {isIncome ? <ArrowUpRight className="size-3.5" /> : <ArrowDownRight className="size-3.5" />}
          {formatBRL(entry.Amount)}
        </span>

        {onDelete && (
          <AlertDialog>
            <AlertDialogTrigger
              render={
                <Button variant="ghost" size="icon-xs" className="text-muted-foreground hover:text-destructive">
                  <Trash2 className="size-3.5" />
                </Button>
              }
            />
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Excluir transação?</AlertDialogTitle>
                <AlertDialogDescription>Esta ação não pode ser desfeita.</AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancelar</AlertDialogCancel>
                <AlertDialogAction variant="destructive" onClick={() => onDelete(entry.EntryID)}>
                  Excluir
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
      </div>
    </div>
  )
}
