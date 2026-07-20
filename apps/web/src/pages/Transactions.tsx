import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Search, Receipt, Plus } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { useEntriesInfinite, useMarkPaidMutation } from '../api/queries'
import EmptyState from '../components/EmptyState'
import EntriesTable from '../components/EntriesTable'

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

export default function Transactions() {
  const {
    data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage,
  } = useEntriesInfinite()
  const markPaid = useMarkPaidMutation()
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
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight">Transações</h1>
          <p className="mt-1 text-muted-foreground">Todas as entradas e saídas registradas</p>
        </div>
        <Button render={<Link to="/nova-transacao" />} nativeButton={false}>
          <Plus className="size-4" /> Nova Transação
        </Button>
      </div>

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
            <>
              <EntriesTable entries={filtered} onMarkPaid={id => markPaid.mutate(id)} />
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
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
