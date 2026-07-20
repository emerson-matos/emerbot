import { cn } from '@/lib/utils'
import { notificationToneVar, type AppNotification } from '@/lib/notifications'

interface Props {
  notifications: AppNotification[]
  // The bell popover insets rows and highlights on hover; the page renders them
  // flush inside an already-padded card.
  inset?: boolean
}

// Shared row layout for the alert feed, used by both the header bell popover
// and the Notificações page so the two never drift apart.
export default function NotificationList({ notifications, inset = false }: Props) {
  return (
    <div className="divide-y divide-border">
      {notifications.map(n => {
        const Icon = n.icon
        const color = notificationToneVar[n.tone]
        return (
          <div
            key={n.id}
            className={cn(
              'flex items-start gap-2.5 py-3',
              inset && 'px-4 transition-colors hover:bg-muted',
            )}
          >
            <span
              className="grid size-7 shrink-0 place-items-center rounded-md"
              style={{
                background: `color-mix(in oklch, ${color} 15%, transparent)`,
                color,
              }}
            >
              <Icon className="size-3.5" />
            </span>
            <div className="min-w-0">
              <p className="text-[13px] leading-snug">{n.text}</p>
              <p className="mt-0.5 text-[11px] text-muted-foreground">{n.time}</p>
            </div>
          </div>
        )
      })}
    </div>
  )
}
