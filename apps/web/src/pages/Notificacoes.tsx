import { useEffect, useState } from 'react'
import { Bell, CheckCircle2, History, MessageCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useAuth } from '@/lib/auth'
import EmptyState from '../components/EmptyState'
import NotificationList from '../components/NotificationList'
import { useNotifications } from '@/lib/notifications'
import { useNotificationPrefs, useSaveNotificationPrefsMutation } from '../api/queries'

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

function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative h-6 w-11 shrink-0 rounded-full transition-colors',
        checked ? 'bg-success' : 'bg-muted',
      )}
    >
      <span
        className={cn(
          'absolute top-0.5 size-5 rounded-full bg-white transition-[left]',
          checked ? 'left-5.5' : 'left-0.5',
        )}
      />
    </button>
  )
}

const ALERT_CHECKS = [
  { key: 'notifyDueToday', label: 'Uma conta vence hoje' },
  { key: 'notifyOverdue', label: 'Uma conta está vencida' },
  { key: 'notifyGoal', label: 'A meta do mês for atingida' },
] as const

function WhatsAppPreferences() {
  const { user } = useAuth()
  const prefsQuery = useNotificationPrefs()
  const save = useSaveNotificationPrefsMutation()

  const [waEnabled, setWaEnabled] = useState(false)
  const [checks, setChecks] = useState({
    notifyDueToday: true,
    notifyOverdue: true,
    notifyGoal: false,
  })
  const [saved, setSaved] = useState(false)

  // Seed the form once prefs load.
  useEffect(() => {
    const p = prefsQuery.data
    if (!p) return
    setWaEnabled(p.waEnabled)
    setChecks({
      notifyDueToday: p.notifyDueToday,
      notifyOverdue: p.notifyOverdue,
      notifyGoal: p.notifyGoal,
    })
  }, [prefsQuery.data])

  // The delivery number is always the phone registered on the Cognito
  // account — there's nothing to type here, only to enable/disable.
  const phone = user?.phone ?? ''
  const missingPhone = waEnabled && phone.replace(/\D/g, '').length < 10

  function submit() {
    save.mutate(
      { waEnabled, ...checks },
      { onSuccess: () => setSaved(true) },
    )
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <MessageCircle className="size-4 text-primary" aria-hidden />
          Alertas por WhatsApp
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between border-b border-border pb-4">
          <div>
            <p className="text-sm font-medium">Ativar alertas</p>
            <p className="text-xs text-muted-foreground">Receba avisos no seu WhatsApp</p>
          </div>
          <Toggle
            checked={waEnabled}
            onChange={v => { setWaEnabled(v); setSaved(false) }}
            label="Ativar alertas por WhatsApp"
          />
        </div>

        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">
            Número de WhatsApp
          </p>
          <p className={cn('text-sm', !phone && 'text-muted-foreground italic')}>
            {phone ? formatPhoneBR(phone) : 'Nenhum número cadastrado na sua conta'}
          </p>
          <p className="text-xs text-muted-foreground">
            Os alertas são enviados para o telefone da sua conta. Para
            trocá-lo, atualize seu cadastro.
          </p>
          {missingPhone && (
            <p className="text-xs text-destructive">
              Cadastre um número na sua conta para ativar os alertas.
            </p>
          )}
        </div>

        <p className="rounded-lg bg-muted/60 px-3 py-2 text-xs text-muted-foreground">
          Por regra do WhatsApp, os alertas só são enviados por um período após
          você mandar uma mensagem ao bot. Se pararem de chegar, mande qualquer
          mensagem (ex.:{' '}
          <span className="rounded bg-background px-1 py-0.5 font-mono font-medium text-foreground">/resumo</span>
          ) para reativar.
        </p>

        <div className="space-y-2.5">
          <p className="text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">
            Enviar alerta quando
          </p>
          {ALERT_CHECKS.map(({ key, label }) => (
            <label key={key} className="flex cursor-pointer items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-primary"
                checked={checks[key]}
                onChange={e => {
                  setChecks(c => ({ ...c, [key]: e.target.checked }))
                  setSaved(false)
                }}
              />
              {label}
            </label>
          ))}
        </div>

        <div className="flex items-center gap-3 pt-1">
          <Button onClick={submit} disabled={save.isPending || missingPhone}>
            Salvar Preferências
          </Button>
          {saved && !save.isPending && (
            <span className="flex items-center gap-1.5 text-sm text-success">
              <CheckCircle2 className="size-4" aria-hidden />
              Preferências salvas
            </span>
          )}
        </div>
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
        <WhatsAppPreferences />

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
