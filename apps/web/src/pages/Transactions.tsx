import { type SyntheticEvent, useCallback, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { CalendarIcon, ChevronDown, ChevronUp, Plus, Receipt, Search, X } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { Calendar } from '@/components/ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { type DateRange } from 'react-day-picker'
import { useCategories, useEntriesByMonth, useMarkPaidMutation, useDeleteEntryMutation } from '../api/queries'
import EmptyState from '../components/EmptyState'
import PaymentList from '../components/payments/PaymentList'
import type { PaymentGroupData } from '../components/payments/PaymentGroup'
import { bucketByUrgency, effectiveDate, netAmount } from '@/lib/entries'
import { formatBRL, formatSignedBRL, parseAmountToCents } from '@/lib/format'
import { categoriesByType } from '@/lib/categories'
import type { Entry, IncomeOrigin } from '../api/types'
import { incomeOriginLabels } from '../api/types'

type TypeFilter = 'all' | 'income' | 'expense'
type OriginFilter = 'all' | IncomeOrigin
type StatusFilter = 'all' | 'paid' | 'pending' | 'overdue'

const typeLabels: Record<TypeFilter, string> = {
  all: 'Todos os tipos',
  income: 'Entradas',
  expense: 'Saídas',
}

const originLabels: Record<OriginFilter, string> = {
  all: 'Todas as origens',
  ...incomeOriginLabels,
}

const statusLabels: Record<StatusFilter, string> = {
  all: 'Todos os status',
  paid: 'Pago',
  pending: 'Pendente',
  overdue: 'Vencido',
}

type FilterState = {
  search: string
  type: TypeFilter
  status: StatusFilter
  category: string
  origin: OriginFilter
  date: DateRange | undefined
  minAmount: string
  maxAmount: string
}

const defaultFilters: FilterState = {
  search: '',
  type: 'all',
  status: 'all',
  category: 'all',
  origin: 'all',
  date: undefined,
  minAmount: '',
  maxAmount: '',
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

  const [filters, setFilters] = useState<FilterState>(defaultFilters)
  const [pending, setPending] = useState<FilterState>(defaultFilters)

  const updatePending = useCallback((partial: Partial<FilterState>) => {
    setPending(prev => ({ ...prev, ...partial }))
  }, [])

  // Amounts are compared in centavos, the unit the API stores. An unparseable
  // field parses to null and simply doesn't constrain the list — the input is
  // flagged instead, so a typo never looks like "no results".
  const minCents = parseAmountToCents(filters.minAmount)
  const maxCents = parseAmountToCents(filters.maxAmount)
  const minInvalid = pending.minAmount.trim() !== '' && parseAmountToCents(pending.minAmount) === null
  const maxInvalid = pending.maxAmount.trim() !== '' && parseAmountToCents(pending.maxAmount) === null

  const pages = useMemo(() => data?.pages ?? [], [data])
  const currentMonthKey = format(new Date(), 'yyyy-MM')
  const todayISO = format(new Date(), 'yyyy-MM-dd')
  const allEntries = useMemo(() => pages.flatMap(p => p.entries), [pages])

  const matchesFilters = useCallback((e: Entry) => {
    if (filters.type !== 'all' && e.Type !== filters.type) return false
    if (filters.status === 'overdue') {
      const overdue = e.PaymentStatus === 'pending' && (effectiveDate(e) ?? '') < todayISO
      if (!overdue) return false
    } else if (filters.status !== 'all' && e.PaymentStatus !== filters.status) return false
    if (filters.category !== 'all' && e.Category !== filters.category) return false
    if (filters.origin !== 'all' && e.Origin !== filters.origin) return false
    if (filters.date?.from) {
      const d = effectiveDate(e)
      if (!d) return false
      if (d < format(filters.date.from, 'yyyy-MM-dd')) return false
      if (filters.date.to && d > format(filters.date.to, 'yyyy-MM-dd')) return false
    }
    if (minCents !== null && e.Amount < minCents) return false
    if (maxCents !== null && e.Amount > maxCents) return false
    if (filters.search !== '' && !e.Description.toLowerCase().includes(filters.search.toLowerCase())) return false
    return true
  }, [filters, minCents, maxCents, todayISO])

  const categoryOptions = useMemo(() => {
    const list = pending.type === 'all' ? allCategories : categoriesByType(allCategories, pending.type)
    return list.map(c => [c.Slug, c.Label] as const).sort((a, b) => a[1].localeCompare(b[1]))
  }, [allCategories, pending.type])

  const hasActiveFilters = filters.search !== '' || filters.type !== 'all' || filters.status !== 'all'
    || filters.category !== 'all' || filters.origin !== 'all' || filters.date !== undefined
    || filters.minAmount !== '' || filters.maxAmount !== ''

  function handleSubmit(e: SyntheticEvent) {
    e.preventDefault()
    setFilters(pending)
  }

  function clearFilters() {
    setFilters(defaultFilters)
    setPending(defaultFilters)
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
  // The ledger strip splits the filtered set the way a caixa is read: money in,
  // money out, and the balance between them.
  const { entradas, saidas } = useMemo(() => {
    let entradas = 0
    let saidas = 0
    for (const e of filteredEntries) {
      if (e.Type === 'income') entradas += e.Amount
      else saidas += e.Amount
    }
    return { entradas, saidas }
  }, [filteredEntries])

  // Dropping a chip clears one applied filter and its pending twin together, so
  // the list and the controls never disagree about what's active.
  const dropFilter = useCallback((partial: Partial<FilterState>) => {
    setFilters(prev => ({ ...prev, ...partial }))
    setPending(prev => ({ ...prev, ...partial }))
  }, [])

  const activeChips = useMemo(() => {
    const chips: { key: string; label: string; onRemove: () => void }[] = []
    if (filters.search) {
      chips.push({ key: 'search', label: `"${filters.search}"`, onRemove: () => dropFilter({ search: '' }) })
    }
    if (filters.type !== 'all') {
      // Type carries origin: an income-only view is the only place origin means
      // anything, so clearing the type clears the origin with it.
      chips.push({ key: 'type', label: typeLabels[filters.type], onRemove: () => dropFilter({ type: 'all', origin: 'all' }) })
    }
    if (filters.origin !== 'all') {
      chips.push({ key: 'origin', label: `Origem: ${originLabels[filters.origin]}`, onRemove: () => dropFilter({ origin: 'all' }) })
    }
    if (filters.status !== 'all') {
      chips.push({ key: 'status', label: statusLabels[filters.status], onRemove: () => dropFilter({ status: 'all' }) })
    }
    if (filters.category !== 'all') {
      const label = allCategories.find(c => c.Slug === filters.category)?.Label ?? filters.category
      chips.push({ key: 'category', label, onRemove: () => dropFilter({ category: 'all' }) })
    }
    if (filters.date?.from) {
      const label = filters.date.to
        ? `${format(filters.date.from, 'dd/MM/yy')} – ${format(filters.date.to, 'dd/MM/yy')}`
        : format(filters.date.from, 'dd/MM/yy')
      chips.push({ key: 'date', label, onRemove: () => dropFilter({ date: undefined }) })
    }
    if (filters.minAmount) {
      chips.push({ key: 'min', label: `≥ R$ ${filters.minAmount}`, onRemove: () => dropFilter({ minAmount: '' }) })
    }
    if (filters.maxAmount) {
      chips.push({ key: 'max', label: `≤ R$ ${filters.maxAmount}`, onRemove: () => dropFilter({ maxAmount: '' }) })
    }
    return chips
  }, [filters, allCategories, dropFilter])

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

      <form onSubmit={handleSubmit} className="space-y-3">
        <Card>
          <CardContent className="space-y-4">
            {/* Busca + período: os dois filtros mais usados, sempre em destaque. */}
            <div className="flex flex-col gap-3 sm:flex-row">
              <div className="relative flex-1">
                <Search
                  className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
                  aria-hidden
                />
                <Input
                  value={pending.search}
                  onChange={e => updatePending({ search: e.target.value })}
                  placeholder="Buscar descrição ou categoria..."
                  className="h-8 pl-9"
                />
              </div>
              <Popover>
                <PopoverTrigger
                  render={
                    <Button variant="outline" className="h-8 justify-start px-2.5 font-normal sm:w-64">
                      <CalendarIcon data-icon="inline-start" />
                      {pending.date?.from ? (
                        pending.date.to ? (
                          <>{format(pending.date.from, 'dd/MM/y')} – {format(pending.date.to, 'dd/MM/y')}</>
                        ) : (
                          <>{format(pending.date.from, 'dd/MM/y')}</>
                        )
                      ) : (
                        <span className="text-muted-foreground">
                          Período <span className="text-xs">· por vencimento</span>
                        </span>
                      )}
                    </Button>
                  }
                />
                <PopoverContent className="w-auto p-0" align="start">
                  <Calendar
                    mode="range"
                    defaultMonth={pending.date?.from}
                    selected={pending.date}
                    onSelect={value => updatePending({ date: value })}
                    numberOfMonths={2}
                    // Outside days repeat a date in both months (July 30 shows in
                    // July's grid and again as August's leading day), so a single
                    // pick looked selected on both sides. Hide them.
                    showOutsideDays={false}
                  />
                </PopoverContent>
              </Popover>
            </div>

            {/* Refinamentos numa grade fixa: nada reflowa quando um filtro muda. */}
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <Select items={typeLabels} value={pending.type} onValueChange={value => updatePending(value === 'expense' ? { type: 'expense', origin: 'all' } : { type: value as TypeFilter })}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(typeLabels) as TypeFilter[]).map(key => (
                    <SelectItem key={key} value={key}>{typeLabels[key]}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {/* Origem só faz sentido para entradas: fica desabilitada (não some)
                  quando o filtro é Saídas, para a grade não pular de lugar. */}
              <Select items={originLabels} value={pending.origin} disabled={pending.type === 'expense'} onValueChange={value => updatePending({ origin: value as OriginFilter })}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(originLabels) as OriginFilter[]).map(key => (
                    <SelectItem key={key} value={key}>{originLabels[key]}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select items={statusLabels} value={pending.status} onValueChange={value => updatePending({ status: value as StatusFilter })}>
                <SelectTrigger className="w-full">
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
                value={pending.category}
                onValueChange={value => updatePending({ category: value ?? 'all' })}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todas as categorias</SelectItem>
                  {categoryOptions.map(([value, label]) => (
                    <SelectItem key={value} value={value}>{label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Faixa de valor + ações, ancoradas à direita. */}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
              <div className="flex items-center gap-2">
                <span className="shrink-0 text-sm text-muted-foreground">Valor</span>
                <div className="relative">
                  <span className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-sm text-muted-foreground">R$</span>
                  <Input
                    value={pending.minAmount}
                    onChange={e => updatePending({ minAmount: e.target.value })}
                    inputMode="decimal"
                    aria-label="Valor mínimo"
                    aria-invalid={minInvalid}
                    placeholder="mín."
                    className="h-8 w-full pl-8 sm:w-28"
                  />
                </div>
                <span className="shrink-0 text-sm text-muted-foreground">até</span>
                <div className="relative">
                  <span className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-sm text-muted-foreground">R$</span>
                  <Input
                    value={pending.maxAmount}
                    onChange={e => updatePending({ maxAmount: e.target.value })}
                    inputMode="decimal"
                    aria-label="Valor máximo"
                    aria-invalid={maxInvalid}
                    placeholder="máx."
                    className="h-8 w-full pl-8 sm:w-28"
                  />
                </div>
              </div>
              <div className="flex gap-2 sm:ml-auto">
                <Button variant="secondary" type="button" className="h-8" onClick={clearFilters} disabled={!hasActiveFilters}>
                  <X className="size-3.5" /> Limpar
                </Button>
                <Button type="submit" className="h-8">Aplicar filtros</Button>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Tira-resumo do recorte, lida como uma linha de livro-caixa. */}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
          <span className="text-muted-foreground">
            {summaryCount} {summaryCount === 1 ? 'lançamento' : 'lançamentos'}
          </span>
          {summaryCount > 0 && (
            <>
              <Separator orientation="vertical" className="h-4" />
              <span className="text-muted-foreground">
                entradas <span className="font-medium text-success tabular-nums">{formatBRL(entradas)}</span>
              </span>
              <span className="text-muted-foreground">
                saídas <span className="font-medium text-destructive tabular-nums">{formatBRL(saidas)}</span>
              </span>
              <span className="text-muted-foreground">
                saldo{' '}
                <span className={`font-semibold tabular-nums ${summaryNet >= 0 ? 'text-success' : 'text-destructive'}`}>
                  {formatSignedBRL(summaryNet)}
                </span>
              </span>
            </>
          )}
        </div>

        {/* Filtros ativos: visíveis de relance, removíveis um a um. */}
        {activeChips.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            {activeChips.map(chip => (
              <Badge key={chip.key} variant="secondary" className="gap-1 pr-1 font-normal">
                {chip.label}
                <button
                  type="button"
                  onClick={chip.onRemove}
                  aria-label={`Remover filtro ${chip.label}`}
                  className="rounded-full p-0.5 text-muted-foreground transition-colors hover:bg-foreground/10 hover:text-foreground"
                >
                  <X className="size-3" />
                </button>
              </Badge>
            ))}
          </div>
        )}
      </form>

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
                onMarkPaid={entry => markPaid.mutate(entry)}
                onDelete={entry => deleteEntry.mutate(entry)}
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
