import { AlertCircle } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

export default function ErrorState({
  icon: Icon,
  message,
  className = '',
}: {
  icon?: LucideIcon
  message: string
  className?: string
}) {
  return (
    <div className={`flex flex-col items-center justify-center gap-2 text-center ${className}`}>
      <span className="grid size-10 place-items-center rounded-full bg-destructive/10 text-destructive">
        {Icon ? <Icon className="size-5" /> : <AlertCircle className="size-5" />}
      </span>
      <p className="max-w-[24ch] text-sm text-destructive">{message}</p>
    </div>
  )
}
