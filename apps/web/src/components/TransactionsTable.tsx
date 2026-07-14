import { Receipt, ArrowUpRight, ArrowDownRight, Check } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatBRL } from '../api/client'
import type { Entry } from '../api/client'
import { format, parseISO } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import EmptyState from './EmptyState'

interface Props {
  entries: Entry[]
  onMarkPaid?: (id: string) => void
}

const categoryLabels: Record<string, string> = {
  aluguel: 'Aluguel',
  folha_pagamento: 'Folha',
  fornecedor_medicamentos: 'Fornec. Med.',
  fornecedor_geral: 'Fornec. Geral',
  impostos: 'Impostos',
  emprestimo: 'Empréstimo',
  cartao_credito: 'Cartão',
  energia_agua: 'Energia/Água',
  telefone_internet: 'Tel./Internet',
  manutencao: 'Manutenção',
  venda_balcao: 'Venda Balcão',
  convenio: 'Convênio',
  delivery: 'Delivery',
  outros_despesas: 'Outros',
  outros_receitas: 'Outros',
}

export default function TransactionsTable({ entries, onMarkPaid }: Props) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Receipt className="size-4 text-primary" aria-hidden />
          Últimas Transações
        </CardTitle>
      </CardHeader>
      <CardContent className="px-0">
        {entries.length === 0 ? (
          <EmptyState icon={Receipt} message="Nenhuma transação encontrada neste período." />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Data</TableHead>
                  <TableHead>Descrição</TableHead>
                  <TableHead>Categoria</TableHead>
                  <TableHead className="text-right">Valor</TableHead>
                  <TableHead className="text-center">Status</TableHead>
                  {onMarkPaid && <TableHead />}
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.map(e => {
                  const isIncome = e.Type === 'income'
                  return (
                    <TableRow key={e.EntryID}>
                      <TableCell className="whitespace-nowrap text-muted-foreground tabular-nums">
                        {format(parseISO(e.Date), 'dd/MM/yy', { locale: ptBR })}
                      </TableCell>
                      <TableCell className="max-w-xs truncate font-medium">{e.Description || '—'}</TableCell>
                      <TableCell>
                        <Badge variant="outline" className="font-normal">
                          {categoryLabels[e.Category] ?? e.Category}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <span
                          className="inline-flex items-center gap-1 font-semibold tabular-nums"
                          style={{ color: isIncome ? 'var(--success)' : 'var(--destructive)' }}
                        >
                          {isIncome
                            ? <ArrowUpRight className="size-3.5" />
                            : <ArrowDownRight className="size-3.5" />}
                          {formatBRL(e.Amount)}
                        </span>
                      </TableCell>
                      <TableCell className="text-center">
                        {e.PaymentStatus === 'paid' ? (
                          <Badge className="bg-success/15 text-success">Pago</Badge>
                        ) : (
                          <Badge className="bg-warning/15 text-warning">Pendente</Badge>
                        )}
                      </TableCell>
                      {onMarkPaid && (
                        <TableCell className="text-right">
                          {e.PaymentStatus === 'pending' && (
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
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
