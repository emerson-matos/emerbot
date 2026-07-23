import { useState } from "react";
import { format } from "date-fns";
import { ChevronDown, ChevronUp, Receipt } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { Entry } from "../api/types";
import { bucketByUrgency } from "@/lib/entries";
import EmptyState from "./EmptyState";
import PaymentList from "./payments/PaymentList";
import type { PaymentGroupData } from "./payments/PaymentGroup";

interface Props {
  entries: Entry[];
  isLoading?: boolean;
  onMarkPaid?: (id: string) => void;
  onDelete?: (id: string) => void;
}

export default function TransactionsTable({ entries, isLoading, onMarkPaid, onDelete }: Props) {
  const [showHistory, setShowHistory] = useState(false);
  const todayISO = format(new Date(), "yyyy-MM-dd");
  const { overdue, dueToday, upcoming, history } = bucketByUrgency(entries, todayISO);

  const groups: PaymentGroupData[] = [];
  if (overdue.length) {
    groups.push({ key: "overdue", label: "Em atraso", kind: "status", tone: "negative", items: overdue });
  }
  if (dueToday.length) {
    groups.push({
      key: "today",
      label: `Hoje · ${format(new Date(), "dd/MM")}`,
      kind: "status",
      tone: "warning",
      items: dueToday,
    });
  }
  if (upcoming.length) {
    groups.push({ key: "upcoming", label: "Próximos vencimentos", kind: "status", tone: "info", items: upcoming });
  }
  if (showHistory && history.length) {
    groups.push({ key: "history", label: "Histórico do mês", kind: "status", tone: "neutral", items: history });
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Receipt className="size-4 text-primary" aria-hidden />
          Transações
        </CardTitle>
      </CardHeader>
      <CardContent className="px-0">
        {isLoading ? (
          <div className="space-y-2 px-6">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-9 rounded-md" />
            ))}
          </div>
        ) : entries.length === 0 ? (
          <EmptyState
            icon={Receipt}
            message="Nenhuma transação encontrada neste período."
          />
        ) : (
          <>
            <PaymentList groups={groups} onMarkPaid={onMarkPaid} onDelete={onDelete} />
            {history.length > 0 && (
              <div className="flex justify-center pt-1">
                <Button variant="ghost" size="sm" onClick={() => setShowHistory(v => !v)}>
                  {showHistory ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
                  {showHistory ? "Ocultar" : "Mostrar"} histórico do mês ({history.length})
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
