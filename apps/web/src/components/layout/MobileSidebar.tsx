import { LogOut, X } from "lucide-react";

import Brand from "./Brand";
import Navigation from "./Navigation";

interface MobileSidebarProps {
  open: boolean;
  onClose: () => void;
  onLogout: () => void;
}

export default function MobileSidebar({
  open,
  onClose,
  onLogout,
}: MobileSidebarProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-40 lg:hidden">
      <div
        className="absolute inset-0 bg-foreground/40 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* oxlint-disable-next-line tailwindcss/enforce-sort-order -- custom animation token has no known sort position */}
      <aside className="absolute inset-y-0 left-0 flex w-64 flex-col gap-6 border-r border-sidebar-border bg-sidebar p-4 animate-toast-in">
        <div className="flex items-center justify-between pt-2">
          <Brand />

          <button
            onClick={onClose}
            aria-label="Fechar menu"
            className="rounded-md p-1.5 text-muted-foreground hover:bg-muted"
          >
            <X className="size-5" />
          </button>
        </div>

        <Navigation onNavigate={onClose} />

        <button
          onClick={onLogout}
          className="mt-auto flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-destructive"
        >
          <LogOut className="size-4" />
          Sair
        </button>
      </aside>
    </div>
  );
}
