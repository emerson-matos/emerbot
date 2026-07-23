import { Children, isValidElement, type ReactNode, type ComponentType } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

export type KpiTone = 'positive' | 'negative' | 'info' | 'warning' | 'neutral' | 'primary'

export const toneVar: Record<KpiTone, string> = {
  positive: 'var(--success)',
  negative: 'var(--destructive)',
  info: 'var(--info)',
  warning: 'var(--warning)',
  neutral: 'var(--muted-foreground)',
  primary: 'var(--primary)',
}

interface KpiCardProps {
  tone?: KpiTone
  isLoading?: boolean
  isError?: boolean
  errorMessage?: string
  className?: string
  children?: ReactNode
}

export default function KpiCard({
  tone = 'neutral',
  isLoading,
  isError,
  errorMessage = 'Erro ao carregar',
  className,
  children,
}: KpiCardProps) {
  const hasActions = Children.toArray(children)
    .filter(isValidElement)
    .some(
      (child) =>
        (child.type as ComponentType)?.displayName === 'KpiCardActions',
    )

  if (isLoading) {
    return (
      <Card className={cn('relative overflow-hidden', className)}>
        <span
          aria-hidden
          className="absolute inset-y-0 left-0 w-1"
          style={{ background: toneVar.neutral }}
        />
        <CardContent className="flex grow items-center justify-center">
          <Skeleton className="size-full rounded-xl" />
        </CardContent>
        {hasActions && <KpiCardActions />}
      </Card>
    )
  }

  if (isError) {
    return (
      <Card className={cn('relative overflow-hidden', className)}>
        <span
          aria-hidden
          className="absolute inset-y-0 left-0 w-1"
          style={{ background: toneVar.negative }}
        />
        <CardContent className="flex grow items-center justify-center">
          <p className="text-xs text-destructive">{errorMessage}</p>
        </CardContent>
        {hasActions && <KpiCardActions />}
      </Card>
    )
  }

  return (
    <Card className={cn('relative overflow-hidden', className)}>
      <span
        aria-hidden
        className="absolute inset-y-0 left-0 w-1"
        style={{ background: toneVar[tone] }}
      />
      {children}
    </Card>
  )
}

interface KpiCardContentProps {
  icon: LucideIcon
  tone: KpiTone
  className?: string
  children?: ReactNode
}

export function KpiCardContent({
  icon: Icon,
  tone,
  className,
  children,
}: KpiCardContentProps) {
  const c = toneVar[tone]
  return (
    <CardContent
      className={cn('flex grow items-start justify-between gap-3 pl-5', className)}
    >
      <div className="min-w-0">{children}</div>
      <span
        className="grid size-9 shrink-0 place-items-center rounded-lg"
        style={{
          background: `color-mix(in oklch, ${c} 14%, transparent)`,
          color: c,
        }}
      >
        <Icon className="size-4.5" />
      </span>
    </CardContent>
  )
}

interface KpiCardActionsProps {
  className?: string
  children?: ReactNode
}

export function KpiCardActions({ className, children }: KpiCardActionsProps) {
  return (
    <div
      className={cn(
        'flex min-h-9 items-center px-(--card-spacing) pl-5',
        className,
      )}
    >
      {children}
    </div>
  )
}
KpiCardActions.displayName = 'KpiCardActions'
