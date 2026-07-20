import { useEffect, useRef, useState } from 'react'
import { Bell } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useNotifications } from '@/lib/notifications'
import NotificationList from './NotificationList'

export default function NotificationBell() {
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  const containerRef = useRef<HTMLDivElement>(null)
  const { notifications, hasNotifications } = useNotifications()

  useEffect(() => {
    if (!open) return

    function onPointerDown(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }

    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div ref={containerRef} className="relative">
      <button
        onClick={() => setOpen(o => !o)}
        aria-label="Notificações"
        aria-expanded={open}
        className="relative grid size-9 shrink-0 place-items-center rounded-lg text-muted-foreground ring-1 ring-foreground/10 transition-colors hover:bg-muted hover:text-foreground"
      >
        <Bell className="size-4" />
        {hasNotifications && (
          <span className="absolute right-1.5 top-1.5 size-2 rounded-full bg-destructive ring-2 ring-background" />
        )}
      </button>

      {open && (
        <div className="absolute right-0 top-11 z-30 w-80 overflow-hidden rounded-xl bg-popover text-popover-foreground shadow-lg ring-1 ring-foreground/10">
          <div className="border-b border-border px-4 py-3 text-sm font-semibold">
            Notificações
          </div>

          <div className="max-h-72 overflow-y-auto">
            {notifications.length === 0 ? (
              <p className="px-4 py-6 text-center text-sm text-muted-foreground">
                Nenhuma notificação por aqui.
              </p>
            ) : (
              <NotificationList notifications={notifications} inset />
            )}
          </div>

          <button
            onClick={() => {
              setOpen(false)
              navigate('/notificacoes')
            }}
            className="w-full border-t border-border px-4 py-2.5 text-[13px] font-medium text-primary transition-colors hover:bg-muted"
          >
            Configurar alertas
          </button>
        </div>
      )}
    </div>
  )
}
