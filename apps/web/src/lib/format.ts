export function formatBRL(centavos: number): string {
  return (centavos / 100).toLocaleString("pt-BR", {
    style: "currency",
    currency: "BRL",
  });
}

export function formatSignedBRL(centavos: number): string {
  const sign = centavos >= 0 ? "+ " : "− ";
  return sign + formatBRL(Math.abs(centavos));
}

// A pt-BR keyboard produces "1.234,56"; a numeric keypad produces "1234.56".
// The pt-BR reading wins, so "1.234" is one thousand two hundred thirty-four.
// Comma grouping is only read as such when a dotted decimal part proves it
// ("1,234.56"), because a bare "10,555" is far more likely a fumbled decimal
// than ten thousand — better rejected than off by a factor of a thousand.
// Group 1 is always the integer part (separators included), group 2 the decimals.
const AMOUNT_PATTERNS = [
  /^(\d{1,3}(?:\.\d{3})+|\d+)(?:,(\d{1,2}))?$/,
  /^(\d{1,3}(?:,\d{3})+)\.(\d{1,2})$/,
  /^(\d+)\.(\d{1,2})$/,
];

/**
 * Parses a user-typed amount in reais into centavos, the unit the API speaks.
 * Returns null for anything that is not a non-negative amount — including the
 * empty string — so callers can tell "no filter" and "typed it wrong" apart
 * instead of silently dropping the input.
 */
export function parseAmountToCents(raw: string): number | null {
  const text = raw.trim().replace(/^R\$/i, "").replace(/\s/g, "");
  if (text === "") return null;

  let match: RegExpExecArray | null = null;
  for (const pattern of AMOUNT_PATTERNS) {
    match = pattern.exec(text);
    if (match) break;
  }
  if (!match) return null;

  const reais = Number(match[1].replace(/[.,]/g, ""));
  const centavos = match[2] ? Number(match[2].padEnd(2, "0")) : 0;
  return reais * 100 + centavos;
}
