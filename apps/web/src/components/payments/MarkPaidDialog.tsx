import { useId, useState, type ReactElement, type ReactNode } from 'react'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  MAX_PAYMENT_METHOD_LENGTH,
  lastPaymentMethod,
  paymentMethodSuggestions,
  rememberPaymentMethod,
} from '@/lib/payment-methods'

interface Props {
  /** The button that opens the dialog; rendered as the trigger itself. */
  trigger: ReactElement
  isIncome: boolean
  title: string
  description: ReactNode
  confirmLabel?: string
  onConfirm: (method: string) => void
}

/**
 * The confirmation that quits a lançamento, with the one question the ledger
 * could not answer before it: how it was paid.
 *
 * The field is optional and free text (ADR-026). It opens pre-filled with the
 * last form used in this browser, so somebody who always pays in pix still
 * quits a bill in one click — and clearing it is just as valid, because "não
 * informado" is the ordinary state of this field, not a gap to be nagged about.
 */
export default function MarkPaidDialog({
  trigger,
  isIncome,
  title,
  description,
  confirmLabel = 'Confirmar',
  onConfirm,
}: Props) {
  const [open, setOpen] = useState(false)
  const [method, setMethod] = useState('')
  const fieldId = useId()
  const listId = `${fieldId}-formas`
  const suggestions = paymentMethodSuggestions()

  // Seeded on open rather than on mount: a list renders dozens of these, and
  // the last used form can change between two of them being opened.
  const handleOpenChange = (next: boolean) => {
    if (next) setMethod(lastPaymentMethod())
    setOpen(next)
  }

  const confirm = () => {
    const value = method.trim()
    rememberPaymentMethod(value)
    setOpen(false)
    onConfirm(value)
  }

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogTrigger render={trigger} />
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>

        <div className="grid gap-2 text-left">
          <Label htmlFor={fieldId}>
            {isIncome ? 'Forma de recebimento' : 'Forma de pagamento'}{' '}
            <span className="font-normal text-muted-foreground">(opcional)</span>
          </Label>
          <Input
            id={fieldId}
            list={listId}
            value={method}
            maxLength={MAX_PAYMENT_METHOD_LENGTH}
            placeholder={isIncome ? 'Ex.: Pix, dinheiro' : 'Ex.: Pix, boleto'}
            onChange={e => setMethod(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') confirm()
            }}
          />
          <datalist id={listId}>
            {suggestions.map(s => (
              <option key={s} value={s} />
            ))}
          </datalist>
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel>Cancelar</AlertDialogCancel>
          <AlertDialogAction onClick={confirm}>{confirmLabel}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
