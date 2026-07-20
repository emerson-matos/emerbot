import { Moon, Sun } from 'lucide-react'
import { useTheme } from '@/lib/theme'
import { Card, CardContent } from '@/components/ui/card'

export default function Settings() {
  const userName = localStorage.getItem('user_name') ?? 'você'
  const userEmail = localStorage.getItem('user_email') ?? '—'
  const userPhone = localStorage.getItem('user_phone') ?? '—'
  const { theme, toggle } = useTheme()
  const initials = userName.trim().slice(0, 2).toUpperCase() || '??'

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Ajustes</h1>
        <p className="mt-1 text-muted-foreground">Gerencie seu perfil e preferências</p>
      </div>

      <Card>
        <CardContent className="space-y-6">
          <div className="flex items-center gap-4">
            <span className="grid size-14 shrink-0 place-items-center rounded-full bg-primary/15 text-lg font-semibold text-primary">
              {initials}
            </span>
            <div className="min-w-0 leading-tight">
              <p className="truncate text-base font-semibold">{userName}</p>
              <p className="text-[11px] text-muted-foreground">Administrador</p>
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <p className="text-xs text-muted-foreground">Nome</p>
              <p className="text-sm font-medium">{userName}</p>
            </div>
            <div>
              <p className="text-xs text-muted-foreground">E-mail</p>
              <p className="text-sm font-medium break-all">{userEmail}</p>
              <p className="text-[11px] text-muted-foreground">Imutável — gerenciado pelo administrador</p>
            </div>
            <div>
              <p className="text-xs text-muted-foreground">Telefone</p>
              <p className="text-sm font-medium">{userPhone}</p>
              <p className="text-[11px] text-muted-foreground">Imutável — gerenciado pelo administrador</p>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent>
          <div className="flex items-center justify-between gap-4">
            <div className="min-w-0 leading-tight">
              <p className="text-sm font-medium">Tema escuro</p>
              <p className="text-xs text-muted-foreground">Alterna a aparência do painel</p>
            </div>
            <button
              onClick={toggle}
              aria-label={theme === 'dark' ? 'Tema claro' : 'Tema escuro'}
              className="grid size-9 shrink-0 place-items-center rounded-lg text-muted-foreground ring-1 ring-foreground/10 transition-colors hover:bg-muted hover:text-foreground"
            >
              {theme === 'dark' ? <Sun className="size-4" /> : <Moon className="size-4" />}
            </button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
