// Weekday names live here, indexed by the number the backend sends
// (time.Weekday / getDay(): 0 = domingo). They used to be keyed by abbreviated
// Portuguese labels that Go itself produced, so the backend's spelling decided
// whether this table matched — and a miss printed the abbreviation instead. A
// weekday is a number on the wire; the words for it are a rendering, and
// rendering belongs to whatever is doing the talking.

export const WEEKDAY_SHORT_PT = [
  'Dom',
  'Seg',
  'Ter',
  'Qua',
  'Qui',
  'Sex',
  'Sáb',
] as const

export const WEEKDAY_FULL_PT = [
  'domingo',
  'segunda',
  'terça',
  'quarta',
  'quinta',
  'sexta',
  'sábado',
] as const

// segunda through sexta are -feira and so feminine; domingo and sábado are not.
// One hardcoded "uma" in the sentence read "para uma domingo" two days a week.
const WEEKDAY_ARTICLE_PT = ['um', 'uma', 'uma', 'uma', 'uma', 'uma', 'um'] as const

export function weekdayShort(day: number): string {
  return WEEKDAY_SHORT_PT[day] ?? ''
}

export function weekdayFull(day: number): string {
  return WEEKDAY_FULL_PT[day] ?? ''
}

export function weekdayWithArticle(day: number): string {
  return `${WEEKDAY_ARTICLE_PT[day] ?? 'uma'} ${weekdayFull(day)}`
}
