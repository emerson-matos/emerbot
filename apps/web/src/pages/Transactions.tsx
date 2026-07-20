import { useState } from 'react'
import { Search, Receipt, ArrowUpRight, ArrowDownRight } from 'lucide-react'
import { format, parseISO, isValid } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { formatBRL } from '../api/client'
import type { Entry } from '../api/client'
import { useEntriesInfinite } from '../api/queries'
import AppLayout from '../components/AppLayout'
import EmptyState from '../components/EmptyState'

type TypeFilter = 'all' | 'income' | 'expense'
type StatusFilter = 'all' | 'paid' | 'pending'

const typeLabels: Record<TypeFilter, string> = {
  all: 'Todos os tipos',
  income: 'Receitas',
  expense: 'Despesas',
}

const statusLabels: Record<StatusFilter, string> = {
  all: 'Todos os status',
  paid: 'Pago',
  pending: 'Pendente',
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

function formatEffectiveDate(e: Entry): string {
  const iso = e.DueDate || e.Date
  if (!iso) return '—'
  const parsed = parseISO(iso)
  return isValid(parsed) ? format(parsed, 'dd/MM/yy', { locale: ptBR }) : '—'
}

export default function Transactions() {
  const {
    data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage,
  } = useEntriesInfinite()
  const entries = data?.pages.flatMap(p => p.entries) ?? []

  const [search, setSearch] = useState('')
  const [type, setType] = useState<TypeFilter>('all')
  const [status, setStatus] = useState<StatusFilter>('all')

  const filtered = entries.filter(e => {
    if (type !== 'all' && e.Type !== type) return false
    if (status !== 'all' && e.PaymentStatus !== status) return false
    if (search !== '' && !e.Description.toLowerCase().includes(search.toLowerCase())) return false
    return true
  })

  return (
    <div className="space-y-6">
      <Card>
        <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <div className="relative flex-1">
            <Search
              className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              value={search}
              onChange={e => setSearch(e.target.value)}
              placeholder="Buscar descrição..."
              className="pl-9"
            />
          </div>
          <Select
            items={typeLabels}
            value={type}
            onValueChange={value => setType(value as TypeFilter)}
          >
            <SelectTrigger className="w-full sm:w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(typeLabels) as TypeFilter[]).map(key => (
                <SelectItem key={key} value={key}>
                  {typeLabels[key]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            items={statusLabels}
            value={status}
            onValueChange={value => setStatus(value as StatusFilter)}
          >
            <SelectTrigger className="w-full sm:w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(statusLabels) as StatusFilter[]).map(key => (
                <SelectItem key={key} value={key}>
                  {statusLabels[key]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="px-0">
          {isLoading ? (
            <div className="space-y-2 px-6">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton key={i} className="h-9 rounded-md" />
              ))}
            </div>
          ) : filtered.length === 0 ? (
            <EmptyState
              icon={Receipt}
              message="Nenhuma transação encontrada."
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
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.map(e => {
                    const isIncome = e.Type === 'income'
                    return (
                      <TableRow key={e.EntryID}>
                        <TableCell className="whitespace-nowrap text-muted-foreground tabular-nums">
                          {formatEffectiveDate(e)}
                        </TableCell>
                        <TableCell className="max-w-xs truncate font-medium">
                          {e.Description || '—'}
                        </TableCell>
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
                            {isIncome ? (
                              <ArrowUpRight className="size-3.5" />
                            ) : (
                              <ArrowDownRight className="size-3.5" />
                            )}
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
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
              {hasNextPage && (
                <div className="flex justify-center pt-3">
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={isFetchingNextPage}
                    onClick={() => fetchNextPage()}
                  >
                    {isFetchingNextPage ? 'Carregando...' : 'Carregar mais'}
                  </Button>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
