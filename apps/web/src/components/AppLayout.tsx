import { useState } from 'react'
import type { ReactNode } from 'react'
import {
  LayoutDashboard, Receipt, Target, Settings, Pill,
  Moon, Sun, LogOut, Menu, X,
} from 'lucide-react'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'

interface NavItem {
  label: string
  icon: typeof LayoutDashboard
  active?: boolean
  soon?: boolean
}

const nav: NavItem[] = [
  { label: 'Painel', icon: LayoutDashboard, active: true },
  { label: 'Transações', icon: Receipt, soon: true },
  { label: 'Metas', icon: Target, soon: true },
  { label: 'Ajustes', icon: Settings, soon: true },
]

function Brand() {
  return (
    <div className="flex items-center gap-2.5 px-2">
      <span className="grid size-9 place-items-center rounded-xl bg-primary text-primary-foreground shadow-sm">
        <Pill className="size-5" />
      </span>
      <div className="leading-tight">
        <p className="font-heading text-sm font-semibold tracking-tight">Farmácia</p>
        <p className="text-[11px] text-muted-foreground">Financeiro</p>
      </div>
    </div>
  )
}

function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="flex flex-col gap-1">
      {nav.map(item => {
        const Icon = item.icon
        return (
          <button
            key={item.label}
            type="button"
            disabled={item.soon}
            onClick={onNavigate}
            aria-current={item.active ? 'page' : undefined}
            className={cn(
              'group flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
              item.active
                ? 'bg-sidebar-accent text-sidebar-accent-foreground'
                : 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground',
              item.soon && 'cursor-not-allowed opacity-50 hover:bg-transparent',
            )}
          >
            <Icon className="size-4 shrink-0" />
            <span className="flex-1 text-left">{item.label}</span>
            {item.soon && (
              <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                em breve
              </span>
            )}
          </button>
        )
      })}
    </nav>
  )
}

export default function AppLayout({
  children,
  userName,
  subtitle,
  onLogout,
}: {
  children: ReactNode
  userName: string
  subtitle?: string
  onLogout: () => void
}) {
  const { theme, toggle } = useTheme()
  const [mobileOpen, setMobileOpen] = useState(false)
  const initials = userName.trim().slice(0, 2).toUpperCase() || '??'

  return (
    <div className="min-h-screen lg:grid lg:grid-cols-[16rem_1fr]">
      {/* Desktop sidebar */}
      <aside className="sticky top-0 hidden h-screen flex-col gap-6 border-r border-sidebar-border bg-sidebar/80 p-4 backdrop-blur lg:flex">
        <div className="pt-2">
          <Brand />
        </div>
        <NavLinks />
        <div className="mt-auto flex items-center gap-3 rounded-xl bg-card/60 p-2.5 ring-1 ring-foreground/5">
          <span className="grid size-8 place-items-center rounded-full bg-primary/15 text-xs font-semibold text-primary">
            {initials}
          </span>
          <div className="min-w-0 flex-1 leading-tight">
            <p className="truncate text-sm font-medium">{userName}</p>
            <p className="text-[11px] text-muted-foreground">Administrador</p>
          </div>
          <button
            onClick={onLogout}
            aria-label="Sair"
            className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-destructive"
          >
            <LogOut className="size-4" />
          </button>
        </div>
      </aside>

      {/* Mobile drawer */}
      {mobileOpen && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div
            className="absolute inset-0 bg-foreground/40 backdrop-blur-sm"
            onClick={() => setMobileOpen(false)}
          />
          <aside className="absolute inset-y-0 left-0 flex w-64 flex-col gap-6 border-r border-sidebar-border bg-sidebar p-4 [animation:toast-in_.2s_ease-out]">
            <div className="flex items-center justify-between pt-2">
              <Brand />
              <button
                onClick={() => setMobileOpen(false)}
                aria-label="Fechar menu"
                className="rounded-md p-1.5 text-muted-foreground hover:bg-muted"
              >
                <X className="size-5" />
              </button>
            </div>
            <NavLinks onNavigate={() => setMobileOpen(false)} />
            <button
              onClick={onLogout}
              className="mt-auto flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-destructive"
            >
              <LogOut className="size-4" /> Sair
            </button>
          </aside>
        </div>
      )}

      <div className="flex min-w-0 flex-col">
        <header className="sticky top-0 z-30 flex items-center gap-3 border-b border-border bg-background/70 px-4 py-3 backdrop-blur-md sm:px-6">
          <button
            onClick={() => setMobileOpen(true)}
            aria-label="Abrir menu"
            className="rounded-md p-1.5 text-muted-foreground hover:bg-muted lg:hidden"
          >
            <Menu className="size-5" />
          </button>
          <div className="min-w-0 flex-1">
            <h1 className="font-heading truncate text-base font-semibold tracking-tight sm:text-lg">
              Painel Financeiro
            </h1>
            {subtitle && (
              <p className="truncate text-xs capitalize text-muted-foreground">{subtitle}</p>
            )}
          </div>
          <button
            onClick={toggle}
            aria-label={theme === 'dark' ? 'Tema claro' : 'Tema escuro'}
            className="grid size-9 place-items-center rounded-lg text-muted-foreground ring-1 ring-foreground/10 transition-colors hover:bg-muted hover:text-foreground"
          >
            {theme === 'dark' ? <Sun className="size-4" /> : <Moon className="size-4" />}
          </button>
        </header>
        <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-6 sm:px-6">{children}</main>
      </div>
    </div>
  )
}
