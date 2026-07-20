import { Bell, Menu, Moon, Sun } from "lucide-react";

interface HeaderProps {
  theme: "light" | "dark";
  onToggleTheme: () => void;
  onOpenMenu: () => void;
}

export default function Header({
  theme,
  onToggleTheme,
  onOpenMenu,
}: HeaderProps) {
  return (
    <header className="sticky top-0 z-30 flex h-16 shrink-0 items-center gap-3 border-b border-border bg-background/70 px-4 backdrop-blur-md sm:px-6">
      <button
        onClick={onOpenMenu}
        aria-label="Abrir menu"
        className="rounded-md p-1.5 text-muted-foreground hover:bg-muted lg:hidden"
      >
        <Menu className="size-5" />
      </button>

      <div className="min-w-0 flex-1" />

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
