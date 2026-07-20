import { Bell, Menu, Moon, Sun } from "lucide-react";
import { useLocation } from "react-router-dom";
import { format } from "date-fns";
import { ptBR } from "date-fns/locale";

interface HeaderProps {
  theme: "light" | "dark";
  onToggleTheme: () => void;
  onOpenMenu: () => void;
}

function capitalizeFirst(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

const PAGE_META: Record<string, { title: string; subtitle: string }> = {
  "/": {
    title: "Painel Financeiro",
    subtitle: capitalizeFirst(format(new Date(), "MMMM 'de' yyyy", { locale: ptBR })),
  },
  "/transacoes": { title: "Transações", subtitle: "Entradas e saídas registradas" },
  "/metas": { title: "Metas", subtitle: "Metas financeiras do mês" },
  "/ajustes": { title: "Ajustes", subtitle: "Perfil e preferências" },
};

export default function Header({
  theme,
  onToggleTheme,
  onOpenMenu,
}: HeaderProps) {
  const { pathname } = useLocation();
  const { title, subtitle } = PAGE_META[pathname] ?? PAGE_META["/"];

  return (
    <header className="sticky top-0 z-30 flex h-16 shrink-0 items-center gap-3 border-b border-border bg-background/70 px-4 backdrop-blur-md sm:px-6">
      <button
        onClick={onOpenMenu}
        aria-label="Abrir menu"
        className="rounded-md p-1.5 text-muted-foreground hover:bg-muted lg:hidden"
      >
        <Menu className="size-5" />
      </button>

      <div className="min-w-0 flex-1">
        <p className="truncate text-base font-semibold tracking-tight">{title}</p>
        <p className="truncate text-xs text-muted-foreground">{subtitle}</p>
      </div>

      <div className="flex items-center gap-2">
        <button
          aria-label="Notificações"
          className="grid size-9 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        >
          <Bell className="size-4" />
        </button>

        <button
          onClick={onToggleTheme}
          aria-label={theme === "dark" ? "Tema claro" : "Tema escuro"}
          className="grid size-9 shrink-0 place-items-center rounded-lg text-muted-foreground ring-1 ring-foreground/10 transition-colors hover:bg-muted hover:text-foreground"
        >
          {theme === "dark"
            ? <Sun className="size-4" />
            : <Moon className="size-4" />}
        </button>
      </div>
    </header>
  );
}
