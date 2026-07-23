import { cn } from '@/lib/utils'
import { toneDotClass, type Tone } from '@/lib/tone'

interface Props {
  tone: Tone
  className?: string
}

export default function StatusDot({ tone, className }: Props) {
  return <span aria-hidden className={cn('size-2 shrink-0 rounded-full', toneDotClass[tone], className)} />
}
