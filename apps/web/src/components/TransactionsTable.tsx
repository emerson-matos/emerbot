import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatBRL } from '../api/client'
import type { Entry } from '../api/client'
import { format, parseISO } from 'date-fns'
import { ptBR } from 'date-fns/locale'

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
        <CardTitle className="text-sm">🧾 Últimas Transações</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Data</TableHead>
              <TableHead>Descrição</TableHead>
              <TableHead>Categoria</TableHead>
              <TableHead className="text-right">Valor</TableHead>
              <TableHead className="text-center">Status</TableHead>
              {onMarkPaid && <TableHead />}
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries.length === 0 && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                  Nenhuma transação encontrada
                </TableCell>
              </TableRow>
            )}
            {entries.map(e => (
              <TableRow key={e.EntryID}>
                <TableCell className="text-muted-foreground whitespace-nowrap">
                  {format(parseISO(e.Date), 'dd/MM/yy', { locale: ptBR })}
                </TableCell>
                <TableCell className="max-w-xs truncate">{e.Description || '—'}</TableCell>
                <TableCell>
                  <Badge variant="secondary" className="font-normal">
                    {categoryLabels[e.Category] ?? e.Category}
                  </Badge>
                </TableCell>
                <TableCell className={`text-right font-medium tabular-nums ${e.Type === 'income' ? 'text-emerald-600' : 'text-red-600'}`}>
                  {e.Type === 'income' ? '+' : '-'}{formatBRL(e.Amount)}
                </TableCell>
                <TableCell className="text-center">
                  {e.PaymentStatus === 'paid' ? (
                    <Badge variant="default" className="bg-emerald-100 text-emerald-700 hover:bg-emerald-100">Pago</Badge>
                  ) : (
                    <Badge variant="outline" className="bg-amber-50 text-amber-700 border-amber-200">Pendente</Badge>
                  )}
                </TableCell>
                {onMarkPaid && (
                  <TableCell className="text-center">
                    {e.PaymentStatus === 'pending' && (
                      <Button variant="link" size="sm" className="text-emerald-600 h-auto p-0" onClick={() => onMarkPaid(e.EntryID)}>
                        Marcar pago
                      </Button>
                    )}
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
