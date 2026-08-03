import type { Entry, IncomeOrigin, UpdateEntryInput } from '../api/types'

/**
 * The fields a lançamento is written with, shared by the create and edit
 * forms so a rule only has to be right once. The two pages lay their inputs
 * out differently — creating is a fast keyboard flow, editing is a correction
 * — but "what counts as a valid entry" must not depend on which one you're on.
 */
export interface EntryFormValues {
  type: 'income' | 'expense'
  description: string
  category: string
  /** Reais as typed into a number input, e.g. "1234.56". */
  amount: string
  /** yyyy-MM-dd. */
  date: string
}

export type EntryFormField = keyof Omit<EntryFormValues, 'type'>

/**
 * Centavos, the unit the API stores, or null when the field does not hold a
 * usable positive amount. Null is a validation failure, never a zero — a
 * lançamento worth R$ 0,00 is a typo, not a transaction.
 */
export function amountToCents(raw: string): number | null {
  const value = Number.parseFloat(raw)
  if (!Number.isFinite(value) || value <= 0) return null
  return Math.round(value * 100)
}

/** The inverse, for prefilling the edit form from a stored entry. */
export function centsToAmountField(centavos: number): string {
  return (centavos / 100).toFixed(2)
}

export function entryFormErrors(values: EntryFormValues): Record<EntryFormField, boolean> {
  return {
    description: !values.description.trim(),
    category: !values.category,
    amount: amountToCents(values.amount) === null,
    date: !values.date,
  }
}

/**
 * What blocks a save on the edit page — the create rules minus the
 * description.
 *
 * Requiring one there is right: it is our form, and an unlabelled row in a
 * ledger is bad data. Requiring it *here* would be a trap. Neither the API nor
 * the AI's add_entry tool has ever required a description (see the tool's
 * Required list in packages/finance/tools.go), so entries reach this page
 * without one — and demanding it back means somebody who opened the page to fix
 * a wrong amount cannot save until they also invent a label for an entry the
 * bot wrote. A correction must never be blocked by a field the user did not
 * come to touch.
 */
export function editEntryErrors(values: EntryFormValues): Record<EntryFormField, boolean> {
  return { ...entryFormErrors(values), description: false }
}

/** The form values that describe an existing entry, ready to be edited. */
export function entryToFormValues(entry: Entry): EntryFormValues {
  return {
    type: entry.Type,
    description: entry.Description,
    category: entry.Category,
    amount: centsToAmountField(entry.Amount),
    date: entry.TransactionDate,
  }
}

/**
 * Every editable field of an entry: the shared ones plus those only the edit
 * page offers.
 */
export interface EditEntryValues extends EntryFormValues {
  origin: IncomeOrigin | ''
  /** yyyy-MM-dd, or "" for no due date. */
  dueDate: string
  status: 'pending' | 'paid'
  supplier: string
}

export function entryToEditValues(entry: Entry): EditEntryValues {
  return {
    ...entryToFormValues(entry),
    origin: entry.Origin ?? '',
    dueDate: entry.DueDate ?? '',
    status: entry.PaymentStatus,
    supplier: entry.Supplier,
  }
}

/**
 * The patch to send for an edit: only the fields that actually changed.
 *
 * Sending the whole form back would be an edit of every field, and the API
 * reads a present key as an instruction. The one that matters is `origin`: an
 * entry written before the field existed has none, and re-sending its blank
 * form value would be a deliberate write of "no origin" over data a later
 * backfill is meant to set. Untouched means unsent.
 */
export function editEntryPatch(
  initial: EditEntryValues,
  current: EditEntryValues,
): UpdateEntryInput {
  const patch: UpdateEntryInput = {}

  if (current.type !== initial.type) patch.type = current.type
  if (current.description !== initial.description) patch.description = current.description.trim()
  if (current.category !== initial.category) patch.category = current.category
  if (current.date !== initial.date) patch.date = current.date
  if (current.dueDate !== initial.dueDate) patch.due_date = current.dueDate
  if (current.status !== initial.status) patch.payment_status = current.status
  if (current.supplier !== initial.supplier) patch.supplier = current.supplier

  const amount = amountToCents(current.amount)
  if (amount !== null && amount !== amountToCents(initial.amount)) patch.amount = amount

  // Origin only exists on income. When an entry becomes an expense the server
  // drops it, so sending one would be noise at best and a rejected request at
  // worst.
  if (current.type === 'income' && current.origin !== initial.origin) {
    patch.origin = current.origin as IncomeOrigin
  }

  return patch
}

export function isEmptyPatch(patch: UpdateEntryInput): boolean {
  return Object.keys(patch).length === 0
}
