import { NavLink } from "react-router-dom";
import {
  ArrowLeftRight,
  Bell,
  CreditCard,
  Landmark,
  LayoutDashboard,
  PlusCircle,
  Receipt,
  Settings,
  Target,
} from "lucide-react";

import { cn } from "@/lib/utils";

interface NavItem {
  label: string;
  icon: typeof LayoutDashboard;
  to?: string;
  soon?: boolean;
}

const nav: NavItem[] = [
  { label: "Painel", icon: LayoutDashboard, to: "/" },
  { label: "Transações", icon: Receipt, to: "/transacoes" },
  { label: "Nova Transação", icon: PlusCircle, to: "/nova-transacao" },
  { label: "Adquirentes", icon: CreditCard, to: "/adquirentes" },
  { label: "Metas", icon: Target, to: "/metas" },
  { label: "Notificações", icon: Bell, to: "/notificacoes" },
  { label: "Estoque", icon: ArrowLeftRight, soon: true },
  { label: "Contas", icon: Landmark, soon: true },
  { label: "Ajustes", icon: Settings, to: "/ajustes" },
];

interface NavigationProps {
  onNavigate?: () => void;
}

export default function Navigation({
  onNavigate,
}: NavigationProps) {
  const base =
    "group flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors";

  const inactive =
    "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground";

  return (
    <nav className="flex flex-col gap-1">
      {nav.map((item) => {
        const Icon = item.icon;

        if (item.soon) {
          return (
            <button
              key={item.label}
              disabled
              type="button"
              className={cn(
                base,
                inactive,
                "cursor-not-allowed opacity-50 hover:bg-transparent"
              )}
            >
              <Icon className="size-4 shrink-0" />

              <span className="flex-1 text-left">
                {item.label}
              </span>

              <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                em breve
              </span>
            </button>
          );
        }

        return (
          <NavLink
            key={item.label}
            to={item.to!}
            end={item.to === "/"}
            onClick={onNavigate}
            className={({ isActive }) =>
              cn(
                base,
                isActive
                  ? "bg-sidebar-primary text-sidebar-primary-foreground"
                  : inactive
              )
            }
          >
            <Icon className="size-4 shrink-0" />

            <span className="flex-1 text-left">
              {item.label}
            </span>
          </NavLink>
        );
      })}
    </nav>
  );
}
