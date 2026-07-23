export type Tone = 'positive' | 'negative' | 'warning' | 'info' | 'neutral'

export const toneDotClass: Record<Tone, string> = {
  positive: 'bg-success',
  negative: 'bg-destructive',
  warning: 'bg-warning',
  info: 'bg-info',
  neutral: 'bg-muted-foreground',
}

export const toneTextClass: Record<Tone, string> = {
  positive: 'text-success',
  negative: 'text-destructive',
  warning: 'text-warning',
  info: 'text-info',
  neutral: 'text-muted-foreground',
}
