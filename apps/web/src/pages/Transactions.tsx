import { useCallback, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { ChevronDown, ChevronUp, Plus, Receipt, Search, X } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { useCategories, useEntriesByMonth, useMarkPaidMutation, useDeleteEntryMutation } from '../api/queries'
import EmptyState from '../components/EmptyState'
import PaymentList from '../components/payments/PaymentList'
import type { PaymentGroupData } from '../components/payments/PaymentGroup'
import { bucketByUrgency, effectiveDate, netAmount } from '@/lib/entries'
import { formatSignedBRL } from '@/lib/format'
import { categoriesByType } from '@/lib/categories'
import type { Entry } from '../api/types'

type TypeFilter = 'all' | 'income' | 'expense'
type StatusFilter = 'all' | 'paid' | 'pending' | 'overdue'

const typeLabels: Record<TypeFilter, string> = {
  all: 'Todos os tipos',
  income: 'Receitas',
  expense: 'Despesas',
}

const statusLabels: Record<StatusFilter, string> = {
  all: 'Todos os status',
  paid: 'Pago',
  pending: 'Pendente',
  overdue: 'Vencido',
}

function monthLabel(monthKey: string): string {
  return format(new Date(`${monthKey}-01T00:00:00`), 'MMMM yyyy', { locale: ptBR })
}

export default function Transactions() {
  const {
    data, isLoading,
    fetchNextPage, hasNextPage, isFetchingNextPage,
    fetchPreviousPage, hasPreviousPage, isFetchingPreviousPage,
  } = useEntriesByMonth()
  const categoriesQuery = useCategories()
  const allCategories = useMemo(() => categoriesQuery.data ?? [], [categoriesQuery.data])
  const markPaid = useMarkPaidMutation()
  const deleteEntry = useDeleteEntryMutation()

  const [search, setSearch] = useState('')
  const [type, setType] = useState<TypeFilter>('all')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [category, setCategory] = useState('all')
  const [month, setMonth] = useState('all')

  const pages = useMemo(() => data?.pages ?? [], [data])
  const currentMonthKey = format(new Date(), 'yyyy-MM')
  const todayISO = format(new Date(), 'yyyy-MM-dd')
  const allEntries = useMemo(() => pages.flatMap(p => p.entries), [pages])

  const matchesFilters = useCallback((e: Entry) => {
    if (type !== 'all' && e.Type !== type) return false
    if (status === 'overdue') {
      const overdue = e.PaymentStatus === 'pending' && (effectiveDate(e) ?? '') < todayISO
      if (!overdue) return false
    } else if (status !== 'all' && e.PaymentStatus !== status) return false
    if (category !== 'all' && e.Category !== category) return false
    if (month !== 'all' && (effectiveDate(e) ?? '').slice(0, 7) !== month) return false
    if (search !== '' && !e.Description.toLowerCase().includes(search.toLowerCase())) return false
    return true
  }, [type, status, category, month, search, todayISO])

  const categoryOptions = useMemo(() => {
    const list = type === 'all' ? allCategories : categoriesByType(allCategories, type)
    return list.map(c => [c.Slug, c.Label] as const).sort((a, b) => a[1].localeCompare(b[1]))
  }, [allCategories, type])

  const monthOptions = useMemo(() => {
    const set = new Set<string>()
    allEntries.forEach(e => set.add((effectiveDate(e) ?? '').slice(0, 7)))
    return [...set].filter(Boolean).sort().reverse()
  }, [allEntries])

  const hasActiveFilters = search !== '' || type !== 'all' || status !== 'all' || category !== 'all' || month !== 'all'

  function clearFilters() {
    setSearch('')
    setType('all')
    setStatus('all')
    setCategory('all')
    setMonth('all')
  }

  const groups: PaymentGroupData[] = useMemo(() => {
    const result: PaymentGroupData[] = []

    const futurePages = pages.filter(p => p.month > currentMonthKey).sort((a, b) => a.month.localeCompare(b.month))
    for (const p of futurePages) {
      const items = p.entries.filter(matchesFilters)
      if (items.length) {
        result.push({ key: p.month, label: monthLabel(p.month), kind: 'period', tone: 'info', items })
      }
    }

    const currentPage = pages.find(p => p.month === currentMonthKey)
    if (currentPage) {
      const filtered = currentPage.entries.filter(matchesFilters)
      const { overdue, dueToday, upcoming, history } = bucketByUrgency(filtered, todayISO)
      if (upcoming.length) {
        result.push({ key: 'upcoming', label: 'Próximos vencimentos', kind: 'status', tone: 'info', items: upcoming })
      }
      if (dueToday.length) {
        result.push({
          key: 'today',
          label: `Hoje · ${format(new Date(), 'dd/MM')}`,
          kind: 'status',
          tone: 'warning',
          items: dueToday,
        })
      }
      if (overdue.length) {
        result.push({ key: 'overdue', label: 'Em atraso', kind: 'status', tone: 'negative', items: overdue })
      }
      if (history.length) {
        result.push({ key: 'history', label: 'Histórico do mês', kind: 'status', tone: 'neutral', items: history })
      }
    }

    const pastPages = pages.filter(p => p.month < currentMonthKey).sort((a, b) => b.month.localeCompare(a.month))
    for (const p of pastPages) {
      const items = p.entries.filter(matchesFilters)
      if (items.length) {
        result.push({ key: p.month, label: monthLabel(p.month), kind: 'period', tone: 'neutral', items })
      }
    }

    return result
  }, [pages, currentMonthKey, todayISO, matchesFilters])

  const filteredEntries = useMemo(
    () => allEntries.filter(matchesFilters),
    [allEntries, matchesFilters],
  )
  const summaryCount = filteredEntries.length
  const summaryNet = netAmount(filteredEntries)

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
        <CardContent className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
          <div className="relative flex-1 sm:min-w-55">
            <Search
              className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              value={search}
              onChange={e => setSearch(e.target.value)}
              placeholder="Buscar descrição ou categoria..."
              className="pl-9"
            />
          </div>
          <Select items={typeLabels} value={type} onValueChange={value => setType(value as TypeFilter)}>
            <SelectTrigger className="w-full sm:w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(typeLabels) as TypeFilter[]).map(key => (
                <SelectItem key={key} value={key}>{typeLabels[key]}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select items={statusLabels} value={status} onValueChange={value => setStatus(value as StatusFilter)}>
            <SelectTrigger className="w-full sm:w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(statusLabels) as StatusFilter[]).map(key => (
                <SelectItem key={key} value={key}>{statusLabels[key]}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            items={{ all: 'Todas as categorias', ...Object.fromEntries(categoryOptions) }}
            value={category}
            onValueChange={value => setCategory(value ?? 'all')}
          >
            <SelectTrigger className="w-full sm:w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Todas as categorias</SelectItem>
              {categoryOptions.map(([value, label]) => (
                <SelectItem key={value} value={value}>{label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            items={{ all: 'Todos os períodos', ...Object.fromEntries(monthOptions.map(k => [k, monthLabel(k)])) }}
            value={month}
            onValueChange={value => setMonth(value ?? 'all')}
          >
            <SelectTrigger className="w-full sm:w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Todos os períodos</SelectItem>
              {monthOptions.map(key => (
                <SelectItem key={key} value={key} className="capitalize">{monthLabel(key)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </CardContent>
      </Card>

      <div className="flex flex-wrap items-center gap-3 text-sm">
        <span className="text-muted-foreground">
          {summaryCount} {summaryCount === 1 ? 'lançamento' : 'lançamentos'}
        </span>
        {summaryCount > 0 && (
          <span className={`font-semibold tabular-nums ${summaryNet >= 0 ? 'text-success' : 'text-destructive'}`}>
            Saldo do período: {formatSignedBRL(summaryNet)}
          </span>
        )}
        {hasActiveFilters && (
          <Button variant="ghost" size="sm" onClick={clearFilters}>
            <X className="size-3.5" /> Limpar filtros
          </Button>
        )}
      </div>

      <Card>
        <CardContent className="px-0">
          {isLoading ? (
            <div className="space-y-2 px-6">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton key={i} className="h-9 rounded-md" />
              ))}
            </div>
          ) : groups.length === 0 ? (
            <EmptyState icon={Receipt} message="Nenhuma transação encontrada." />
          ) : (
            <>
              {hasNextPage && (
                <div className="flex justify-center pb-1">
                  <Button variant="ghost" size="sm" disabled={isFetchingNextPage} onClick={() => fetchNextPage()}>
                    <ChevronUp className="size-3.5" />
                    {isFetchingNextPage ? 'Carregando...' : 'Carregar registros futuros'}
                  </Button>
                </div>
              )}
              <PaymentList
                groups={groups}
                onMarkPaid={id => markPaid.mutate(id)}
                onDelete={id => deleteEntry.mutate(id)}
              />
              {hasPreviousPage && (
                <div className="flex justify-center pt-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={isFetchingPreviousPage}
                    onClick={() => fetchPreviousPage()}
                  >
                    <ChevronDown className="size-3.5" />
                    {isFetchingPreviousPage ? 'Carregando...' : 'Carregar registros anteriores'}
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
