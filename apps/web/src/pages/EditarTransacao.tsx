import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { format, isValid, parseISO } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { Repeat, Receipt, TrendingDown, TrendingUp } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Combobox, ComboboxInput, ComboboxContent, ComboboxList, ComboboxItem, ComboboxEmpty,
} from '@/components/ui/combobox'
import EmptyState from '../components/EmptyState'
import { categoriesByType } from '@/lib/categories'
import {
  editEntryErrors, editEntryPatch, entryToEditValues, isEmptyPatch,
  type EditEntryValues,
} from '@/lib/entry-form'
import { useCategories, useEntry, useUpdateEntryMutation } from '../api/queries'
import type { Category, IncomeOrigin } from '../api/types'
import { incomeOriginLabels } from '../api/types'

function formatDay(iso: string | null): string {
  if (!iso) return ''
  const parsed = parseISO(iso)
  return isValid(parsed) ? format(parsed, "dd 'de' MMMM", { locale: ptBR }) : ''
}

export default function EditarTransacao() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const entryQuery = useEntry(id)
  const categoriesQuery = useCategories()
  const updateEntry = useUpdateEntryMutation(id ?? '')

  // The stored entry is the baseline the patch is diffed against, so the form
  // is only seeded once and edits are never overwritten by a refetch.
  const [initial, setInitial] = useState<EditEntryValues | null>(null)
  const [values, setValues] = useState<EditEntryValues | null>(null)

  const entry = entryQuery.data
  if (entry && !initial) {
    const seeded = entryToEditValues(entry)
    setInitial(seeded)
    setValues(seeded)
  }

  const categories = useMemo(() => categoriesQuery.data ?? [], [categoriesQuery.data])
  const categoryLabels = useMemo(
    () => Object.fromEntries(categories.map(c => [c.Slug, c.Label])),
    [categories],
  )
  const categoriesForType = categoriesByType(categories, values?.type ?? 'expense')

  if (entryQuery.isLoading || (entry && !values)) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-64" />
        <Card className="max-w-xl">
          <CardContent className="space-y-4">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-md" />
            ))}
          </CardContent>
        </Card>
      </div>
    )
  }

  if (entryQuery.isError || !entry || !values || !initial) {
    return (
      <div className="space-y-6">
        <h1 className="text-3xl font-semibold tracking-tight">Editar Transação</h1>
        <Card>
          <CardContent className="px-0">
            <EmptyState icon={Receipt} message="Lançamento não encontrado." />
          </CardContent>
        </Card>
        <Button variant="ghost" size="sm" onClick={() => navigate('/transacoes')}>
          Voltar para Transações
        </Button>
      </div>
    )
  }

  const errors = editEntryErrors(values)
  const patch = editEntryPatch(initial, values)
  const nothingToSave = isEmptyPatch(patch)
  const invalid = Object.values(errors).some(Boolean)
  const isIncome = values.type === 'income'
  const inSeries = Boolean(entry.RecurrenceID)

  function update(partial: Partial<EditEntryValues>) {
    setValues(prev => (prev ? { ...prev, ...partial } : prev))
  }

  function changeType(next: 'income' | 'expense') {
    // Categories are per type, so a category from the old side would be saved
    // against a type that does not offer it.
    const first = categoriesByType(categories, next)[0]?.Slug ?? ''
    update({ type: next, category: first })
  }

  async function submit() {
    if (invalid || nothingToSave) return

    try {
      await updateEntry.mutateAsync(patch)
      navigate('/transacoes')
    } catch {
      // The mutation's onError toast is the whole story.
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Editar Transação</h1>
        <p className="mt-1 text-muted-foreground">Corrija os dados deste lançamento</p>
      </div>

      {inSeries && (
        <div className="flex max-w-xl items-start gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2.5 text-sm">
          <Repeat className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
          <p className="text-muted-foreground">
            {entry.RecurrenceIndex && entry.RecurrenceTotal
              ? `Parcela ${entry.RecurrenceIndex} de ${entry.RecurrenceTotal} de uma série recorrente. `
              : 'Este lançamento faz parte de uma série recorrente. '}
            A alteração vale só para esta parcela — as outras continuam como estão.
          </p>
        </div>
      )}

      <Card className="max-w-xl">
        <CardContent>
          <form onSubmit={e => { e.preventDefault(); submit() }} className="space-y-4">
            <div>
              <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">Tipo</Label>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  className={isIncome ? 'flex-1 border-success bg-success/15 text-success' : 'flex-1 border-success text-success'}
                  onClick={() => changeType('income')}
                >
                  <TrendingUp className="size-3.5" /> Receita
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className={!isIncome ? 'flex-1 border-destructive bg-destructive/15 text-destructive' : 'flex-1 border-destructive text-destructive'}
                  onClick={() => changeType('expense')}
                >
                  <TrendingDown className="size-3.5" /> Despesa
                </Button>
              </div>
            </div>

            <div>
              <Label htmlFor="tx-desc" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                Descrição
              </Label>
              <Input
                id="tx-desc"
                value={values.description}
                onChange={e => update({ description: e.target.value })}
                placeholder="Ex.: Venda Balcão — Semana 3"
              />
            </div>

            <div>
              <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">Categoria</Label>
              <Combobox
                value={values.category}
                onValueChange={v => update({ category: v as string })}
                itemToStringLabel={(item: string) => categoryLabels[item] ?? item}
              >
                <ComboboxInput
                  placeholder="Buscar categoria..."
                  aria-invalid={errors.category}
                />
                <ComboboxContent>
                  <ComboboxList>
                    {categoriesForType.map((c: Category) => (
                      <ComboboxItem key={c.Slug} value={c.Slug}>
                        {c.Label}
                      </ComboboxItem>
                    ))}
                  </ComboboxList>
                  <ComboboxEmpty>Nenhuma categoria encontrada</ComboboxEmpty>
                </ComboboxContent>
              </Combobox>
              {errors.category && (
                <p className="mt-1 text-xs text-destructive">Selecione uma categoria</p>
              )}
            </div>

            {isIncome && (
              <div>
                <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">Origem</Label>
                <Combobox
                  value={values.origin}
                  onValueChange={v => update({ origin: v as IncomeOrigin })}
                  itemToStringLabel={(item: string) => incomeOriginLabels[item as IncomeOrigin] ?? item}
                >
                  <ComboboxInput placeholder="Buscar origem..." />
                  <ComboboxContent>
                    <ComboboxList>
                      {Object.entries(incomeOriginLabels).map(([slug, label]) => (
                        <ComboboxItem key={slug} value={slug}>
                          {label}
                        </ComboboxItem>
                      ))}
                    </ComboboxList>
                    <ComboboxEmpty>Nenhuma origem encontrada</ComboboxEmpty>
                  </ComboboxContent>
                </Combobox>
                <p className="mt-1.5 text-xs text-muted-foreground">
                  Só <span className="font-medium">Venda</span> conta como faturamento e entra na meta.
                  Corrigir aqui é o que tira um empréstimo lançado como venda do faturamento.
                </p>
              </div>
            )}

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <Label htmlFor="tx-amount" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                  Valor (R$)
                </Label>
                <Input
                  id="tx-amount"
                  type="number"
                  min="0"
                  step="0.01"
                  value={values.amount}
                  onChange={e => update({ amount: e.target.value })}
                  placeholder="0,00"
                  aria-invalid={errors.amount}
                  aria-describedby={errors.amount ? 'amount-error' : undefined}
                />
                {errors.amount && (
                  <p id="amount-error" className="mt-1 text-xs text-destructive">Informe um valor maior que zero</p>
                )}
              </div>
              <div>
                <Label htmlFor="tx-date" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                  Data da transação
                </Label>
                <Input
                  id="tx-date"
                  type="date"
                  value={values.date}
                  onChange={e => update({ date: e.target.value })}
                  aria-invalid={errors.date}
                />
                {errors.date && (
                  <p className="mt-1 text-xs text-destructive">Data obrigatória</p>
                )}
              </div>
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <Label htmlFor="tx-due" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                  Vencimento
                </Label>
                <Input
                  id="tx-due"
                  type="date"
                  value={values.dueDate}
                  onChange={e => update({ dueDate: e.target.value })}
                />
                <p className="mt-1.5 text-xs text-muted-foreground">
                  Deixe em branco para o lançamento valer pela data da transação.
                </p>
              </div>
              {!isIncome && (
                <div>
                  <Label htmlFor="tx-supplier" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                    Fornecedor
                  </Label>
                  <Input
                    id="tx-supplier"
                    value={values.supplier}
                    onChange={e => update({ supplier: e.target.value })}
                    placeholder="Opcional"
                  />
                </div>
              )}
            </div>

            <div>
              <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">Status</Label>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  className={values.status === 'paid' ? 'flex-1 bg-accent' : 'flex-1'}
                  onClick={() => update({ status: 'paid' })}
                >
                  {isIncome ? 'Recebido' : 'Pago'}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className={values.status === 'pending' ? 'flex-1 bg-accent' : 'flex-1'}
                  onClick={() => update({ status: 'pending' })}
                >
                  Pendente
                </Button>
              </div>
              {/* The payment date is the server's to record — it is the day the
                  money moved, not a field to type. */}
              {values.status === 'paid' && entry.PaymentDate && initial.status === 'paid' && (
                <p className="mt-1.5 text-xs text-muted-foreground">
                  {isIncome ? 'Recebido' : 'Pago'} em {formatDay(entry.PaymentDate)}.
                </p>
              )}
              {values.status === 'paid' && initial.status === 'pending' && (
                <p className="mt-1.5 text-xs text-muted-foreground">
                  Será registrado como {isIncome ? 'recebido' : 'pago'} hoje.
                </p>
              )}
            </div>

            <div className="flex items-center gap-3 pt-2">
              <Button type="submit" disabled={updateEntry.isPending || invalid || nothingToSave}>
                Salvar alterações
              </Button>
              <Button
                type="button"
                variant="ghost"
                render={<Link to="/transacoes" />}
                nativeButton={false}
              >
                Cancelar
              </Button>
              {nothingToSave && !invalid && (
                <span className="text-xs text-muted-foreground">Nada alterado</span>
              )}
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
