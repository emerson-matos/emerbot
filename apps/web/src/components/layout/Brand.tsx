import { Pill } from "lucide-react";

export default function Brand() {
	return (
		<div className="flex items-center gap-2.5 px-2">
			<span className="grid size-9 place-items-center rounded-xl bg-primary text-primary-foreground shadow-sm">
				<Pill className="size-5" />
			</span>

			<div className="leading-tight">
				<p className="font-heading text-sm font-semibold tracking-tight">
					Drogaria Nova Farma
				</p>

				<p className="text-[11px] text-muted-foreground">
					Financeiro
				</p>
			</div>
		</div>
	);
}
