import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from 'react'
import { useNavigate } from 'react-router-dom'
import { format } from 'date-fns'
import { TrendingDown, TrendingUp } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import {
  Combobox, ComboboxInput, ComboboxContent, ComboboxList, ComboboxItem, ComboboxEmpty,
} from '@/components/ui/combobox'
import { categoriesByType } from '@/lib/categories'
import { useCategories, useCreateEntryMutation } from '../api/queries'
import type { Category } from '../api/types'
import { IncomeOrigin, incomeOriginLabels } from '../api/types'

type EntryType = 'income' | 'expense'

const initialTouched = {
  desc: false,
  category: false,
  amount: false,
}

type Field = keyof typeof initialTouched

const today = () => format(new Date(), 'yyyy-MM-dd')

export default function NovaTransacao() {
  const navigate = useNavigate()
  const createEntry = useCreateEntryMutation()
  const categoriesQuery = useCategories()

  const descRef = useRef<HTMLInputElement>(null)
  const amountRef = useRef<HTMLInputElement>(null)
  const dateRef = useRef<HTMLInputElement>(null)

  const [type, setType] = useState<EntryType>('income')
  const [desc, setDesc] = useState('')
  const [category, setCategory] = useState('')
  const [amount, setAmount] = useState('')
  const [date, setDate] = useState(today)
  // Only "venda" counts toward faturamento, so this is the field that decides
  // whether an inflow reaches the sales goal. It defaults to a sale because
  // that is what a pharmacy records all day.
  const [origin, setOrigin] = useState<IncomeOrigin>(IncomeOrigin.Venda)
  const [touched, setTouched] = useState<Record<Field, boolean>>(initialTouched)

  const categories = useMemo(() => categoriesQuery.data ?? [], [categoriesQuery.data])

  const incomeCategories = categoriesByType(categories, 'income')
  const expenseCategories = categoriesByType(categories, 'expense')
  const categoriesForType = type === 'income' ? incomeCategories : expenseCategories

  const categoryLabels = useMemo(
    () => Object.fromEntries(categories.map(c => [c.Slug, c.Label])),
    [categories],
  )

  const amountValue = Number.parseFloat(amount)
  const validAmount = Number.isFinite(amountValue) && amountValue > 0

  const errors = {
    desc: !desc.trim(),
    category: !category,
    amount: !validAmount,
    date: !date,
  }

  const invalid = Object.values(errors).some(Boolean)

  function touch(field: Field) {
    setTouched(prev => prev[field] ? prev : { ...prev, [field]: true })
  }

  function firstCategory(type: EntryType) {
    const list = type === 'income' ? incomeCategories : expenseCategories
    return list[0]?.Slug ?? ''
  }

  function resetForm() {
    setDesc('')
    setAmount('')
    setDate(today())
    setOrigin(IncomeOrigin.Venda)
    setTouched(initialTouched)
  }

  function changeType(next: EntryType) {
    setType(next)
    setCategory(firstCategory(next))
    setTouched(initialTouched)
  }

  useEffect(() => {
    if (!category && categoriesForType.length) {
      setCategory(categoriesForType[0].Slug)
    }
  }, [category, categoriesForType])

  async function submit() {
    if (invalid) {
      setTouched({ desc: true, category: true, amount: true })
      return
    }

    try {
      await createEntry.mutateAsync({
        date,
        amount: Math.round(amountValue * 100),
        category,
        type,
        description: desc.trim(),
        origin: type === 'income' ? origin : undefined,
      })

      createEntry.reset()
      resetForm()
      descRef.current?.focus()
    } catch {
      // Mutation state surfaces the error
    }
  }

  const focusNext =
    (next?: RefObject<HTMLInputElement | null>) =>
      (e: KeyboardEvent<HTMLInputElement>) => {
        if (e.key !== 'Enter') return

        e.preventDefault()

        if (next?.current) {
          next.current.focus()
        } else {
          void submit()
        }
      }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Nova Transação</h1>
        <p className="mt-1 text-muted-foreground">Registre uma entrada ou saída</p>
      </div>

      <Card className="max-w-xl">
        <CardContent>
          <form onSubmit={e => { e.preventDefault(); submit() }} className="space-y-4">
            <div>
              <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">Tipo</Label>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  className={type === 'income' ? 'flex-1 border-success bg-success/15 text-success' : 'flex-1 border-success text-success'}
                  onClick={() => changeType('income')}
                >
                  <TrendingUp className="size-3.5" /> Receita
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className={type === 'expense' ? 'flex-1 border-destructive bg-destructive/15 text-destructive' : 'flex-1 border-destructive text-destructive'}
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
                ref={descRef}
                id="tx-desc"
                value={desc}
                onChange={e => { setDesc(e.target.value) }}
                onBlur={() => touch('desc')}
                onKeyDown={focusNext(amountRef)}
                placeholder="Ex.: Venda Balcão — Semana 3"
                aria-invalid={touched.desc && errors.desc}
                aria-describedby={touched.desc && errors.desc ? 'desc-error' : undefined}
              />
              {touched.desc && errors.desc && (
                <p id="desc-error" className="mt-1 text-xs text-destructive">Campo obrigatório</p>
              )}
            </div>

            <div>
              <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">Categoria</Label>
              <Combobox
                value={category}
                onValueChange={v => { setCategory(v as string); touch('category') }}
                itemToStringLabel={(item: string) => categoryLabels[item] ?? item}
              >
                <ComboboxInput
                  placeholder="Buscar categoria..."
                  aria-invalid={touched.category && errors.category}
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
              {touched.category && errors.category && (
                <p className="mt-1 text-xs text-destructive">Selecione uma categoria</p>
              )}
            </div>

            {type === 'income' && (
              <div>
                <Label className="mb-1.5 block text-xs font-medium text-muted-foreground">Origem</Label>
                <Combobox
                  value={origin}
                  onValueChange={v => { setOrigin(v as IncomeOrigin) }}
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
                  Empréstimos e aportes entram apenas nas entradas de caixa.
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
                  ref={amountRef}
                  type="number"
                  min="0"
                  step="0.01"
                  value={amount}
                  onChange={e => { setAmount(e.target.value) }}
                  onBlur={() => touch('amount')}
                  onKeyDown={focusNext(dateRef)}
                  placeholder="0,00"
                  aria-invalid={touched.amount && errors.amount}
                  aria-describedby={touched.amount && errors.amount ? 'amount-error' : undefined}
                />
                {touched.amount && errors.amount && (
                  <p id="amount-error" className="mt-1 text-xs text-destructive">Valor obrigatório</p>
                )}
              </div>
              <div>
                <Label htmlFor="tx-date" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                  Vencimento
                </Label>
                <Input
                  id="tx-date"
                  ref={dateRef}
                  type="date"
                  value={date}
                  onChange={e => { setDate(e.target.value) }}
                  onKeyDown={focusNext()}
                />
              </div>
            </div>

            <div className="flex items-center gap-3 pt-2">
              <Button type="submit" disabled={createEntry.isPending}>
                Salvar Transação
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Button variant="ghost" size="sm" onClick={() => navigate('/transacoes')}>
        Voltar para Transações
      </Button>
    </div>
  )
}
