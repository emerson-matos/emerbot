import { Bell, History, MessageCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import EmptyState from '../components/EmptyState'
import NotificationList from '../components/NotificationList'
import { useNotifications } from '@/lib/notifications'
import { useNotificationPrefs } from '../api/queries'

// Renders the Cognito phone (E.164) as "(11) 98765-4321". Drops the BR
// country code and caps at 11 local digits.
function formatPhoneBR(raw: string): string {
  let d = raw.replace(/\D/g, '')
  if (d.startsWith('55') && d.length > 11) d = d.slice(2)
  d = d.slice(0, 11)
  if (d.length <= 2) return d
  if (d.length <= 7) return `(${d.slice(0, 2)}) ${d.slice(2)}`
  return `(${d.slice(0, 2)}) ${d.slice(2, 7)}-${d.slice(7)}`
}

// What the notifier sends, in the order the day sends it. Written out because
// this card is now the only place someone can find out what to expect on their
// phone — there is nothing to configure, so the page's job is to say what
// arrives and where.
const DAILY_MESSAGES = [
  'Resumo do dia, de manhã',
  'As saídas previstas para hoje, logo em seguida',
  'As saídas ainda em aberto, depois das 15h',
]

function WhatsAppDelivery() {
  // Read from the API rather than from the auth claims, and not only because
  // the API normalizes the number: this request is what registers the account
  // as a recipient (see the Get handler in the dashboard-api). Nobody is
  // messaged until someone has opened this page once.
  const prefsQuery = useNotificationPrefs()
  const phone = prefsQuery.data?.phone ?? ''

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <MessageCircle className="size-4 text-primary" aria-hidden />
          Avisos por WhatsApp
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="border-b border-border pb-4">
          <p className="text-xs font-medium text-muted-foreground">
            As mensagens vão para
          </p>
          {prefsQuery.isLoading ? (
            <p className="mt-1 text-sm text-muted-foreground">Carregando…</p>
          ) : phone ? (
            <p className="mt-1 text-lg font-semibold tabular-nums">
              {formatPhoneBR(phone)}
            </p>
          ) : (
            <p className="mt-1 text-sm text-warning">
              Nenhum número cadastrado na sua conta — sem ele os avisos não têm
              para onde ir.
            </p>
          )}
          <p className="mt-1 text-xs text-muted-foreground">
            É o número da sua conta. Para trocar, altere o cadastro.
          </p>
        </div>

        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">
            Você recebe todo dia
          </p>
          <ul className="space-y-1.5">
            {DAILY_MESSAGES.map(message => (
              <li key={message} className="flex gap-2 text-sm">
                <span className="text-muted-foreground" aria-hidden>
                  •
                </span>
                {message}
              </li>
            ))}
          </ul>
        </div>

        {/* The 20h window is the one thing that can silence a day, and it is
            fixed by the reader rather than by us — so it is stated here instead
            of leaving an unexplained gap on the phone. */}
        <p className="border-t border-border pt-3 text-xs text-muted-foreground">
          Os avisos só saem se você tiver conversado com o bot nas últimas 20
          horas — é uma regra do WhatsApp. Mande qualquer mensagem para ele e a
          janela reabre.
        </p>
      </CardContent>
    </Card>
  )
}

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
        <WhatsAppDelivery />

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
