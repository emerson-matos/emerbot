import { useMemo, useState } from 'react'
import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { usePaymentsSales, usePaymentsReceivables } from '../api/queries'
import {
  SalesCard, FeesCard, ReceivablesCard, ForecastCard,
} from '../components/PaymentsCards'
import { currentMonthRange, monthRange } from '@/lib/period'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Table, TableHeader, TableBody, TableRow, TableHead, TableCell,
} from '@/components/ui/table'
import { formatBRL } from '@/lib/format'

const methodLabels: Record<string, string> = {
  credito: 'Crédito',
  debito: 'Débito',
  pix: 'Pix',
  boleto: 'Boleto',
  outros: 'Outros',
}

// A muted, method-specific chip so the table scans by payment type at a glance.
const methodBadge: Record<string, string> = {
  credito: 'bg-violet-500/10 text-violet-600 dark:text-violet-400',
  debito: 'bg-sky-500/10 text-sky-600 dark:text-sky-400',
  pix: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
  boleto: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
  outros: 'bg-muted text-muted-foreground',
}

// sale_date arrives as YYYY-MM-DD; reorder to dd/mm/yyyy without constructing a
// Date (which would shift by timezone and can land on the previous day).
function formatDate(iso: string): string {
  const [y, m, d] = iso.split('-')
  return `${d}/${m}/${y}`
}

export default function Adquirentes() {
  const [month, setMonth] = useState(() => currentMonthRange().month)

  // The last 12 months, newest first — the window a user realistically browses.
  const monthOptions = useMemo(() => {
    const now = new Date()
    return Array.from({ length: 12 }, (_, i) => {
      const d = new Date(now.getFullYear(), now.getMonth() - i, 1)
      const raw = format(d, 'MMMM yyyy', { locale: ptBR })
      return { key: format(d, 'yyyy-MM'), label: raw.charAt(0).toUpperCase() + raw.slice(1) }
    })
  }, [])
  const monthItems = useMemo(
    () => Object.fromEntries(monthOptions.map((o) => [o.key, o.label])),
    [monthOptions],
  )
  const monthLabel = monthItems[month] ?? month

  const { firstDay, lastDay } = monthRange(month)
  const range = { firstDay, lastDay, monthLabel }

  const salesQuery = usePaymentsSales(firstDay, lastDay)
  // Memoized on the query data rather than written as `?? []` inline: the
  // fallback would be a new array on every render, which defeats the memo below
  // and rebuilds the lookup map each time.
  const sales = useMemo(() => salesQuery.data?.sales ?? [], [salesQuery.data])
  const byMethod = salesQuery.data?.by_method ?? {}
  const methods = Object.entries(byMethod).sort((a, b) => b[1] - a[1])

  const receivablesQuery = usePaymentsReceivables(firstDay, lastDay)
  const receivables = receivablesQuery.data?.receivables ?? []
  // A receivable only carries its sale_id; join back to the loaded sales to show
  // the same meio/bandeira columns as the sales table. Sales settled in a prior
  // month won't be in this window, so the lookup falls back to '—'.
  const saleById = useMemo(() => new Map(sales.map((s) => [s.id, s])), [sales])

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight">Adquirentes</h1>
          <p className="mt-1 text-muted-foreground">
            Vendas, recebíveis e projeção de caixa importados das maquininhas (PagBank).
          </p>
        </div>
        <Select items={monthItems} value={month} onValueChange={(v) => setMonth(v as string)}>
          <SelectTrigger className="w-full sm:w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {monthOptions.map((o) => (
              <SelectItem key={o.key} value={o.key}>{o.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <SalesCard {...range} />
        <FeesCard {...range} />
        <ReceivablesCard {...range} />
        <ForecastCard month={month} monthLabel={monthLabel} />
      </div>

      <Card>
        <CardContent className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">
            Vendas por meio de pagamento · {monthLabel}
          </h2>
          {methods.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Sem vendas importadas neste mês.
            </p>
          ) : (
            <ul className="divide-y divide-border">
              {methods.map(([method, value]) => (
                <li key={method} className="flex items-center justify-between py-2 text-sm">
                  <span>{methodLabels[method] ?? method}</span>
                  <span className="font-medium tabular-nums">{formatBRL(value)}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">
            Vendas importadas · {monthLabel}
          </h2>
          {salesQuery.isError ? (
            <p className="text-sm text-destructive">Erro ao carregar vendas.</p>
          ) : salesQuery.isLoading ? (
            <p className="text-sm text-muted-foreground">Carregando…</p>
          ) : sales.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Sem vendas importadas neste mês.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Data</TableHead>
                    <TableHead>Meio</TableHead>
                    <TableHead>Bandeira</TableHead>
                    <TableHead className="text-center">Parcelas</TableHead>
                    <TableHead className="text-right">Bruto</TableHead>
                    <TableHead className="text-right">Taxa</TableHead>
                    <TableHead className="text-right">Líquido</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sales.map((s) => (
                    <TableRow key={s.id}>
                      <TableCell className="whitespace-nowrap tabular-nums">
                        {formatDate(s.sale_date)}
                      </TableCell>
                      <TableCell>
                        <Badge className={methodBadge[s.method] ?? methodBadge.outros}>
                          {methodLabels[s.method] ?? s.method}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-muted-foreground">{s.brand || '—'}</TableCell>
                      <TableCell className="text-center tabular-nums">
                        {s.installments > 1 ? `${s.installments}x` : 'à vista'}
                      </TableCell>
                      <TableCell className="text-right font-medium tabular-nums">
                        {formatBRL(s.gross_amount)}
                      </TableCell>
                      <TableCell className="text-right text-muted-foreground tabular-nums">
                        {formatBRL(s.fee_amount)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatBRL(s.net_amount)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">
            Líquido a receber · {monthLabel}
          </h2>
          {receivablesQuery.isError ? (
            <p className="text-sm text-destructive">Erro ao carregar recebíveis.</p>
          ) : receivablesQuery.isLoading ? (
            <p className="text-sm text-muted-foreground">Carregando…</p>
          ) : receivables.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Sem recebíveis previstos neste mês.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Previsto</TableHead>
                    <TableHead>Meio</TableHead>
                    <TableHead>Bandeira</TableHead>
                    <TableHead className="text-center">Parcela</TableHead>
                    <TableHead className="text-right">Líquido</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {receivables.map((r) => {
                    const sale = saleById.get(r.sale_id)
                    return (
                      <TableRow key={`${r.sale_id}-${r.installment_number}`}>
                        <TableCell className="whitespace-nowrap tabular-nums">
                          {formatDate(r.expected_date)}
                        </TableCell>
                        <TableCell>
                          {sale ? (
                            <Badge className={methodBadge[sale.method] ?? methodBadge.outros}>
                              {methodLabels[sale.method] ?? sale.method}
                            </Badge>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="text-muted-foreground">{sale?.brand || '—'}</TableCell>
                        <TableCell className="text-center tabular-nums">
                          {r.installment_number}/{r.installment_total}
                        </TableCell>
                        <TableCell className="text-right font-medium tabular-nums">
                          {formatBRL(r.amount)}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
