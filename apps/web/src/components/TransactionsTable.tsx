import { useState } from "react";
import { Receipt } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { Entry } from "../api/types";
import { effectiveDate } from "@/lib/entries";
import EmptyState from "./EmptyState";
import EntriesTable from "./EntriesTable";

interface Props {
  entries: Entry[];
  isLoading?: boolean;
  onMarkPaid?: (id: string) => void;
}

const PAGE_SIZE = 20;

export default function TransactionsTable({ entries, isLoading, onMarkPaid }: Props) {
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE);

  const sorted = [...entries].sort((a, b) => {
    const da = effectiveDate(a) ?? "";
    const db = effectiveDate(b) ?? "";
    return da.localeCompare(db);
  });
  const visible = sorted.slice(0, visibleCount);

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
            <EntriesTable entries={visible} onMarkPaid={onMarkPaid} />
            {visibleCount < sorted.length && (
              <div className="flex justify-center pt-3">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setVisibleCount(c => c + PAGE_SIZE)}
                >
                  Carregar mais
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
