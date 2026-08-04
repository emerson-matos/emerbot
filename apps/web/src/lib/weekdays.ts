// Keyed by the backend's weekday abbreviations (analytics.weekdayLabels).
export const WEEKDAY_FULL_PT: Record<string, string> = {
  Dom: 'domingo',
  Seg: 'segunda',
  Ter: 'terça',
  Qua: 'quarta',
  Qui: 'quinta',
  Sex: 'sexta',
  Sáb: 'sábado',
}

// segunda through sexta are -feira and so feminine; domingo and sábado are not.
// One hardcoded "uma" in the sentence read "para uma domingo" two days a week.
const WEEKDAY_ARTICLE_PT: Record<string, string> = { Dom: 'um', Sáb: 'um' }

export function weekdayWithArticle(label: string): string {
  const name = WEEKDAY_FULL_PT[label]
  if (!name) return label
  return `${WEEKDAY_ARTICLE_PT[label] ?? 'uma'} ${name}`
}
