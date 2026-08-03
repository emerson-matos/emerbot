import { useState } from "react";
import { ChevronDown, ChevronUp, Receipt } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { Entry } from "../api/types";
import { todayISO as computeTodayISO } from "@/lib/entries";
import { urgencyGroups } from "@/lib/payment-groups";
import EmptyState from "./EmptyState";
import PaymentList from "./payments/PaymentList";

interface Props {
  entries: Entry[];
  isLoading?: boolean;
  onMarkPaid?: (entry: Entry) => void;
  onDelete?: (entry: Entry) => void;
}

export default function TransactionsTable({ entries, isLoading, onMarkPaid, onDelete }: Props) {
  const [showHistory, setShowHistory] = useState(false);
  const todayISO = computeTodayISO();

  // The card shows the same groups, in the same order, as the Transações page —
  // it just keeps the settled ones folded away until asked.
  const allGroups = urgencyGroups(entries, todayISO);
  const historyCount = allGroups.find(g => g.key === "history")?.items.length ?? 0;
  const groups = showHistory ? allGroups : allGroups.filter(g => g.key !== "history");

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
            {historyCount > 0 && (
              <div className="flex justify-center pt-1">
                <Button variant="ghost" size="sm" onClick={() => setShowHistory(v => !v)}>
                  {showHistory ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
                  {showHistory ? "Ocultar" : "Mostrar"} histórico do mês ({historyCount})
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
