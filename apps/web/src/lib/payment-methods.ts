/**
 * The forms of payment offered as suggestions when quitting a lançamento.
 *
 * The field itself is free text on purpose (ADR-026): it exists so the person
 * reading their own ledger recognises the movement, not so anything can total
 * by it. Nothing here restricts what can be typed — these are a datalist and a
 * default, which is the cheap defence against the one real hazard of free text,
 * which is drift: "pix", "PIX" and "Pix " becoming three different answers to
 * the same question.
 *
 * The recent list lives in localStorage rather than in the ledger because it is
 * a typing convenience, not data: losing it costs a few keystrokes, and storing
 * it server-side would mean a table, an endpoint and a Query for something the
 * user can retype in four letters.
 */

/** Mirrors domain.MaxPaymentMethodLen; the API answers 400 past it. */
export const MAX_PAYMENT_METHOD_LENGTH = 60

const STORAGE_KEY = 'emerbot.payment-methods.recent'
const MAX_RECENT = 6

/**
 * What a pharmacy actually pays and gets paid with, as a starting point for
 * somebody who has never filled the field in. They are only suggestions: typing
 * "boleto do fornecedor" over them is always allowed.
 */
export const commonPaymentMethods = [
  'Pix',
  'Dinheiro',
  'Cartão de débito',
  'Cartão de crédito',
  'Boleto',
  'Transferência',
]

// localStorage throws on Safari's private mode and when storage is full, and a
// suggestion list is never worth breaking the page it decorates.
function readRecent(): string[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((v): v is string => typeof v === 'string' && v.trim() !== '').slice(0, MAX_RECENT)
  } catch {
    return []
  }
}

function writeRecent(methods: string[]): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(methods.slice(0, MAX_RECENT)))
  } catch {
    // Ignored: see readRecent.
  }
}

/** The forms used most recently, most recent first. */
export function recentPaymentMethods(): string[] {
  return readRecent()
}

/**
 * Records a form as the most recently used one, deduplicating case- and
 * space-insensitively so "pix" typed today does not sit beside the "Pix"
 * clicked yesterday. The spelling kept is the newest one — the user just typed
 * it, so it is the one they mean.
 */
export function rememberPaymentMethod(method: string): void {
  const value = method.trim()
  if (!value) return
  const key = value.toLocaleLowerCase('pt-BR')
  const rest = readRecent().filter(m => m.trim().toLocaleLowerCase('pt-BR') !== key)
  writeRecent([value, ...rest])
}

/**
 * What the datalist offers: what this browser has used, then the common forms
 * it has not. Recent first because a pharmacy that pays everything in pix
 * should find "Pix" at the top on the second day.
 */
export function paymentMethodSuggestions(): string[] {
  const recent = readRecent()
  const seen = new Set(recent.map(m => m.trim().toLocaleLowerCase('pt-BR')))
  return [...recent, ...commonPaymentMethods.filter(m => !seen.has(m.toLocaleLowerCase('pt-BR')))]
}

/**
 * The form to pre-fill the quitação dialog with, so confirming stays one click
 * for whoever always pays the same way. Empty on the first ever use — and empty
 * is a perfectly good answer to leave there.
 */
export function lastPaymentMethod(): string {
  return readRecent()[0] ?? ''
}
