import { useState } from 'react'
import type { ReactNode } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, Receipt, Target, Settings, Pill,
  ArrowLeftRight, Landmark,
  Moon, Sun, LogOut, Menu, X,
  Sidebar,
} from 'lucide-react'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'

interface NavItem {
  label: string
  icon: typeof LayoutDashboard
  to?: string
  soon?: boolean
}

const nav: NavItem[] = [
  { label: 'Painel', icon: LayoutDashboard, to: '/' },
  { label: 'Transações', icon: Receipt, to: '/transacoes' },
  { label: 'Metas', icon: Target, to: '/metas' },
  { label: 'Estoque', icon: ArrowLeftRight, soon: true },
  { label: 'Contas', icon: Landmark, soon: true },
  { label: 'Ajustes', icon: Settings, to: '/ajustes' },
]

function Brand() {
  return (
    <div className="flex items-center gap-2.5 px-2">
      <span className="grid size-9 place-items-center rounded-xl bg-primary text-primary-foreground shadow-sm">
        <Pill className="size-5" />
      </span>
      <div className="leading-tight">
        <p className="font-heading text-sm font-semibold tracking-tight">Drogaria Nova Farma</p>
        <p className="text-[11px] text-muted-foreground">Financeiro</p>
      </div>
    </div>
  )
}

function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  const base = 'group flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors'
  const inactive = 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground'
  return (
    <nav className="flex flex-col gap-1">
      {nav.map(item => {
        const Icon = item.icon
        if (item.soon) {
          return (
            <button
              key={item.label}
              type="button"
              disabled
              className={cn(base, inactive, 'cursor-not-allowed opacity-50 hover:bg-transparent')}
            >
              <Icon className="size-4 shrink-0" />
              <span className="flex-1 text-left">{item.label}</span>
              <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                em breve
              </span>
            </button>
          )
        }
        return (
          <NavLink
            key={item.label}
            to={item.to!}
            end={item.to === '/'}
            onClick={onNavigate}
            className={({ isActive }) =>
              cn(
                base,
                isActive
                  ? 'bg-sidebar-primary text-sidebar-primary-foreground'
                  : inactive,
              )
            }
          >
            <Icon className="size-4 shrink-0" />
            <span className="flex-1 text-left">{item.label}</span>
          </NavLink>
        )
      })}
    </nav>
  )
}

export default function ApppLt() {
  const { theme, toggle } = useTheme()
  const [mobileOpen, setMobileOpen] = useState(false)
  const userName = localStorage.getItem('user_name') ?? 'você'
  const initials = userName?.trim().slice(0, 2).toUpperCase() || '??'
  const navigate = useNavigate();

  function handleLogout() {
    localStorage.clear();
    navigate("/login", { replace: true });
  }
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
            onClick={handleLogout}
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
          <button
            onClick={toggle}
            aria-label={theme === 'dark' ? 'Tema claro' : 'Tema escuro'}
            className="grid size-9 place-items-center rounded-lg text-muted-foreground ring-1 ring-foreground/10 transition-colors hover:bg-muted hover:text-foreground"
          >
            {theme === 'dark' ? <Sun className="size-4" /> : <Moon className="size-4" />}
          </button>
        </header>
        <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-6 sm:px-6"><Outlet /></main>
      </div>
    </div>
  )
}

