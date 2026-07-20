import { useState } from "react";
import { Receipt, ArrowUpRight, ArrowDownRight, Check } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { formatBRL } from "../api/client";
import type { Entry } from "../api/client";
import { format, parseISO, isValid } from "date-fns";
import { ptBR } from "date-fns/locale";
import { categoryLabels } from "@/lib/categories";
import EmptyState from "./EmptyState";

interface Props {
  entries: Entry[];
  isLoading?: boolean;
  onMarkPaid?: (id: string) => void;
}

const PAGE_SIZE = 20;

// The table is about *due* transactions: pending entries show when they're
// due, and already-settled ones (no DueDate) fall back to when they happened.
function effectiveDate(e: Entry): string | null {
  return e.DueDate || e.Date;
}

function formatEffectiveDate(e: Entry): string {
  const iso = effectiveDate(e);
  if (!iso) return "—";
  const parsed = parseISO(iso);
  return isValid(parsed) ? format(parsed, "dd/MM/yy", { locale: ptBR }) : "—";
}

function formatPaidAt(e: Entry): string {
  if (!e.PaymentDate) return "";
  const parsed = parseISO(e.PaymentDate);
  return isValid(parsed) ? `em ${format(parsed, "dd/MM", { locale: ptBR })}` : "";
}

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
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Vencimento</TableHead>
                  <TableHead>Descrição</TableHead>
                  <TableHead>Categoria</TableHead>
                  <TableHead className="text-right">Valor</TableHead>
                  <TableHead className="text-center">Status</TableHead>
                  {onMarkPaid && <TableHead>Pago Em</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.map((e) => {
                  const isIncome = e.Type === "income";
                  return (
                    <TableRow key={e.EntryID}>
                      <TableCell className="whitespace-nowrap text-muted-foreground tabular-nums">
                        {formatEffectiveDate(e)}
                      </TableCell>
                      <TableCell className="max-w-xs truncate font-medium">
                        {e.Description || "—"}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="font-normal">
                          {categoryLabels[e.Category] ?? e.Category}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <span
                          className="inline-flex items-center gap-1 font-semibold tabular-nums"
                          style={{
                            color: isIncome
                              ? "var(--success)"
                              : "var(--destructive)",
                          }}
                        >
                          {isIncome ? (
                            <ArrowUpRight className="size-3.5" />
                          ) : (
                            <ArrowDownRight className="size-3.5" />
                          )}
                          {formatBRL(e.Amount)}
                        </span>
                      </TableCell>
                      <TableCell className="text-center">
                        {e.PaymentStatus === "paid" ? (
                          <Badge className="bg-success/15 text-success">
                            Pago
                          </Badge>
                        ) : (
                          <Badge className="bg-warning/15 text-warning">
                            Pendente
                          </Badge>
                        )}
                      </TableCell>
                      {onMarkPaid && (
                        <TableCell className="whitespace-nowrap">
                          <span className="text-xs text-muted-foreground tabular-nums">
                            {formatPaidAt(e)}
                          </span>
                          {e.PaymentStatus === "pending" && (
                            <Button
                              variant="ghost"
                              size="xs"
                              className="text-success hover:text-success"
                              onClick={() => onMarkPaid(e.EntryID)}
                            >
                              <Check className="size-3.5" /> Pagar
                            </Button>
                          )}
                        </TableCell>
                      )}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
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
          </div>
        )}
      </CardContent>
    </Card>
  );
}
