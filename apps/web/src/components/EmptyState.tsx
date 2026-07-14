import type { LucideIcon } from 'lucide-react'

export default function EmptyState({
  icon: Icon,
  message,
  className = '',
}: {
  icon: LucideIcon
  message: string
  className?: string
}) {
  return (
    <div className={`flex flex-col items-center justify-center gap-2 py-10 text-center ${className}`}>
      <span className="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground">
        <Icon className="size-5" />
      </span>
      <p className="max-w-[24ch] text-sm text-muted-foreground">{message}</p>
    </div>
  )
}
