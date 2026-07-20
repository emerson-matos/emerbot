import { useEffect, useState } from 'react'
import { Bell, CheckCircle2, History, MessageCircle } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import EmptyState from '../components/EmptyState'
import NotificationList from '../components/NotificationList'
import { useNotifications } from '@/lib/notifications'
import { useNotificationPrefs, useSaveNotificationPrefsMutation } from '../api/queries'

// Renders stored/typed phone digits as "(11) 98765-4321". Drops the BR country
// code (the server stores E.164) and caps at 11 local digits.
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
          checked ? 'left-[22px]' : 'left-0.5',
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
  const prefsQuery = useNotificationPrefs()
  const save = useSaveNotificationPrefsMutation()

  const [waEnabled, setWaEnabled] = useState(false)
  const [phone, setPhone] = useState('')
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
    setPhone(formatPhoneBR(p.phone))
    setChecks({
      notifyDueToday: p.notifyDueToday,
      notifyOverdue: p.notifyOverdue,
      notifyGoal: p.notifyGoal,
    })
  }, [prefsQuery.data])

  const phoneDigits = phone.replace(/\D/g, '')
  const missingPhone = waEnabled && phoneDigits.length < 10

  function submit() {
    save.mutate(
      { waEnabled, phone, ...checks },
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
          <label htmlFor="wa-phone" className="text-xs font-medium text-muted-foreground">
            Número de WhatsApp
          </label>
          <Input
            id="wa-phone"
            type="tel"
            inputMode="tel"
            placeholder="(11) 98765-4321"
            value={phone}
            onChange={e => { setPhone(formatPhoneBR(e.target.value)); setSaved(false) }}
            aria-invalid={missingPhone}
          />
          {missingPhone && (
            <p className="text-xs text-destructive">
              Informe um número para ativar os alertas.
            </p>
          )}
        </div>

        <div className="space-y-2.5">
          <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
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
