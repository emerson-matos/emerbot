import { Bell, History, MessageCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import EmptyState from '../components/EmptyState'
import NotificationList from '../components/NotificationList'
import { useNotifications } from '@/lib/notifications'

const WHATSAPP_ALERTS = [
  { label: 'Uma conta vence hoje', color: 'var(--warning)' },
  { label: 'Uma conta está vencida', color: 'var(--destructive)' },
  { label: 'A meta do mês foi atingida', color: 'var(--success)' },
]

export default function Notificacoes() {
  const { notifications, isLoading } = useNotifications()

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Notificações</h1>
        <p className="mt-1 text-muted-foreground">
          Alertas do painel e avisos por WhatsApp
        </p>
      </div>

      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        {/* Phase 2 preview — WhatsApp delivery needs a backend
            (see docs/notifications-phase-2.md). */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <MessageCircle className="size-4 text-primary" aria-hidden />
              Alertas por WhatsApp
              <span className="ml-auto rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium text-muted-foreground">
                em breve
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm text-muted-foreground">
            <p>Em breve você poderá receber estes alertas direto no seu WhatsApp:</p>
            <ul className="space-y-2">
              {WHATSAPP_ALERTS.map(a => (
                <li key={a.label} className="flex items-center gap-2 text-foreground">
                  <span
                    className="size-1.5 shrink-0 rounded-full"
                    style={{ background: a.color }}
                  />
                  {a.label}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>

        {/* Phase 1 — client-derived alert history. */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <History className="size-4 text-primary" aria-hidden />
              Histórico de Alertas
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <p className="py-6 text-center text-sm text-muted-foreground">
                Carregando…
              </p>
            ) : notifications.length === 0 ? (
              <EmptyState icon={Bell} message="Nenhuma notificação por aqui." />
            ) : (
              <NotificationList notifications={notifications} />
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
