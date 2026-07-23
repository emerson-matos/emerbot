import { ArrowDownRight, ArrowUpRight, Check, Trash2 } from "lucide-react";
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
import {
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogFooter,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogCancel,
  AlertDialogAction,
} from "@/components/ui/alert-dialog";
import { formatBRL } from "@/lib/format";
import type { Entry } from "../api/types";
import { categoryLabels } from "@/lib/categories";
import { formatEffectiveDate, formatPaidAt } from "@/lib/entries";

interface Props {
  entries: Entry[];
  onMarkPaid?: (id: string) => void;
  onDelete?: (id: string) => void;
}

// Shared row/column layout for both the dashboard's "Transações" widget and
// the full Transações page — callers own sorting/filtering, loading and
// empty states, and pagination; this just renders the matrix.
export default function EntriesTable({ entries, onMarkPaid, onDelete }: Props) {
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>Vencimento</TableHead>
            <TableHead>Descrição</TableHead>
            <TableHead>Categoria</TableHead>
            <TableHead className="text-right">Valor</TableHead>
            <TableHead className="text-center">Status</TableHead>
            <TableHead>Pago Em</TableHead>
            <TableHead className="w-10" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {entries.map((e) => {
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
                      color: isIncome ? "var(--success)" : "var(--destructive)",
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
                    <Badge className="bg-success/15 text-success">Pago</Badge>
                  ) : (
                    <Badge className="bg-warning/15 text-warning">Pendente</Badge>
                  )}
                </TableCell>
                <TableCell className="whitespace-nowrap">
                  {e.PaymentStatus === "paid" ? (
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {formatPaidAt(e)}
                    </span>
                  ) : onMarkPaid ? (
                    <Button
                      variant="ghost"
                      size="xs"
                      className="text-success hover:text-success"
                      onClick={() => onMarkPaid(e.EntryID)}
                    >
                      <Check className="size-3.5" /> Pagar
                    </Button>
                  ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <AlertDialog>
                    <AlertDialogTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          className="text-muted-foreground hover:text-destructive"
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      }
                    />
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>Excluir transação?</AlertDialogTitle>
                        <AlertDialogDescription>
                          Esta ação não pode ser desfeita.
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>Cancelar</AlertDialogCancel>
                        <AlertDialogAction
                          variant="destructive"
                          onClick={() => onDelete?.(e.EntryID)}
                        >
                          Excluir
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
