import { LogOut } from "lucide-react";
import Brand from "./Brand";
import Navigation from "./Navigation";

interface SidebarProps {
	userName: string;
	initials: string;
	onLogout: () => void;
}

export default function Sidebar({
	userName,
	initials,
	onLogout,
}: SidebarProps) {
	return (
		<aside className="sticky top-0 hidden h-screen flex-col gap-6 border-r border-sidebar-border bg-sidebar/80 p-4 backdrop-blur lg:flex">
			<div className="pt-2">
				<Brand />
			</div>

			<Navigation />

			<div className="mt-auto flex items-center gap-3 rounded-xl bg-card/60 p-2.5 ring-1 ring-foreground/5">
				<span className="grid size-8 place-items-center rounded-full bg-primary/15 text-xs font-semibold text-primary">
					{initials}
				</span>

				<div className="min-w-0 flex-1 leading-tight">
					<p className="truncate text-sm font-medium">{userName}</p>
					<p className="text-[11px] text-muted-foreground">
						Administrador
					</p>
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
	);
}
