import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { format } from 'date-fns'
import { CheckCircle2, TrendingDown, TrendingUp } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import {
  Combobox, ComboboxInput, ComboboxContent, ComboboxList, ComboboxItem, ComboboxEmpty,
} from '@/components/ui/combobox'
import { categoriesByType } from '@/lib/categories'
import { useCategories, useCreateEntryMutation } from '../api/queries'
import type { Category } from '../api/types'

type EntryType = 'income' | 'expense'

export default function NovaTransacao() {
  const navigate = useNavigate()
  const createEntry = useCreateEntryMutation()
  const categoriesQuery = useCategories()
  const categories = useMemo(() => categoriesQuery.data ?? [], [categoriesQuery.data])

  const [type, setType] = useState<EntryType>('income')
  const [desc, setDesc] = useState('')
  const [category, setCategory] = useState('')
  const [amount, setAmount] = useState('')
  const [date, setDate] = useState(format(new Date(), 'yyyy-MM-dd'))
  const [created, setCreated] = useState(false)
  const [touched, setTouched] = useState<Record<string, boolean>>({})

  const categoriesForType = useMemo(() => categoriesByType(categories, type), [categories, type])

  // Seed the category once the list loads (it's empty on first render).
  useEffect(() => {
    if (!category && categoriesForType.length) setCategory(categoriesForType[0].Slug)
  }, [category, categoriesForType])

  function selectType(next: EntryType) {
    setType(next)
    const first = categoriesByType(categories, next)[0]
    setCategory(first ? first.Slug : '')
    setCreated(false)
    setTouched({})
  }

  function touch(field: string) {
    setTouched(prev => ({ ...prev, [field]: true }))
  }

  const invalid = !desc.trim() || !date || !category || !(Number(amount) > 0)

  function submit() {
    if (invalid) return
    createEntry.mutate({
      date,
      amount: Math.round(Number(amount) * 100),
      category,
      type,
      description: desc.trim(),
    }, {
      onSuccess: () => {
        setCreated(true)
        setDesc('')
        setAmount('')
        setDate(format(new Date(), 'yyyy-MM-dd'))
        setTouched({})
        document.getElementById('tx-desc')?.focus()
      },
    })
  }

  const categoryLabelMap = useMemo(() => Object.fromEntries(categoriesForType.map(c => [c.Slug, c.Label])), [categoriesForType])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Nova Transação</h1>
        <p className="mt-1 text-muted-foreground">Registre uma entrada ou saída</p>
      </div>

      <Card className="max-w-xl">
        <CardContent className="space-y-4">
          <div>
            <label className="mb-1.5 block text-xs font-medium text-muted-foreground">Tipo</label>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="outline"
                className={type === 'income' ? 'flex-1 border-success bg-success/15 text-success' : 'flex-1 border-success text-success'}
                onClick={() => selectType('income')}
              >
                <TrendingUp className="size-3.5" /> Receita
              </Button>
              <Button
                type="button"
                variant="outline"
                className={type === 'expense' ? 'flex-1 border-destructive bg-destructive/15 text-destructive' : 'flex-1 border-destructive text-destructive'}
                onClick={() => selectType('expense')}
              >
                <TrendingDown className="size-3.5" /> Despesa
              </Button>
            </div>
          </div>

          <div>
            <label htmlFor="tx-desc" className="mb-1.5 block text-xs font-medium text-muted-foreground">
              Descrição
            </label>
            <Input
              id="tx-desc"
              value={desc}
              onChange={e => { setDesc(e.target.value); setCreated(false) }}
              onBlur={() => touch('desc')}
              onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); document.getElementById('tx-amount')?.focus() } }}
              placeholder="Ex.: Venda Balcão — Semana 3"
              aria-invalid={touched.desc && !desc.trim()}
            />
            {touched.desc && !desc.trim() && (
              <p className="mt-1 text-xs text-destructive">Campo obrigatório</p>
            )}
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-medium text-muted-foreground">Categoria</label>
            <Combobox
              value={category}
              onValueChange={v => { setCategory(v as string); setCreated(false); touch('category') }}
              itemToStringLabel={(item: string) => categoryLabelMap[item] ?? item}
            >
              <ComboboxInput
                placeholder="Buscar categoria..."
                aria-invalid={touched.category && !category}
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
            {touched.category && !category && (
              <p className="mt-1 text-xs text-destructive">Selecione uma categoria</p>
            )}
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label htmlFor="tx-amount" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                Valor (R$)
              </label>
              <Input
                id="tx-amount"
                type="number"
                min="0"
                step="0.01"
                value={amount}
                onChange={e => { setAmount(e.target.value); setCreated(false) }}
                onBlur={() => touch('amount')}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); document.getElementById('tx-date')?.focus() } }}
                placeholder="0,00"
                aria-invalid={touched.amount && !(Number(amount) > 0)}
              />
              {touched.amount && !(Number(amount) > 0) && (
                <p className="mt-1 text-xs text-destructive">Valor obrigatório</p>
              )}
            </div>
            <div>
              <label htmlFor="tx-date" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                Vencimento
              </label>
              <Input
                id="tx-date"
                type="date"
                value={date}
                onChange={e => { setDate(e.target.value); setCreated(false) }}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); submit() } }}
              />
            </div>
          </div>

          <div className="flex items-center gap-3 pt-2">
            <Button onClick={submit} disabled={invalid || createEntry.isPending}>
              Salvar Transação
            </Button>
            {created && (
              <span className="flex items-center gap-1.5 text-sm text-success">
                <CheckCircle2 className="size-4" aria-hidden />
                Transação registrada
              </span>
            )}
          </div>
        </CardContent>
      </Card>

      <Button variant="ghost" size="sm" onClick={() => navigate('/transacoes')}>
        Voltar para Transações
      </Button>
    </div>
  )
}
