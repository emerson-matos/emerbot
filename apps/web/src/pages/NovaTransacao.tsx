import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { format } from 'date-fns'
import { CheckCircle2, TrendingDown, TrendingUp } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { categoriesByType } from '@/lib/categories'
import { useCategories, useCreateEntryMutation } from '../api/queries'

type EntryType = 'income' | 'expense'
type Status = 'pending' | 'paid'

const statusLabels: Record<Status, string> = { pending: 'Pendente', paid: 'Pago' }

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
  const [status, setStatus] = useState<Status>('pending')
  const [created, setCreated] = useState(false)

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
  }

  const invalid = !desc.trim() || !date || !category || !(Number(amount) > 0)

  function submit() {
    if (invalid) return
    createEntry.mutate({
      date,
      due_date: status === 'pending' ? date : undefined,
      amount: Math.round(Number(amount) * 100),
      category,
      type,
      description: desc.trim(),
      payment_status: status,
    }, {
      onSuccess: () => {
        setCreated(true)
        setDesc('')
        setAmount('')
        setDate(format(new Date(), 'yyyy-MM-dd'))
        setStatus('pending')
      },
    })
  }

  const categoryItems = Object.fromEntries(categoriesForType.map(c => [c.Slug, c.Label]))

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
              placeholder="Ex.: Venda Balcão — Semana 3"
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="mb-1.5 block text-xs font-medium text-muted-foreground">Categoria</label>
              <Select
                items={categoryItems}
                value={category}
                onValueChange={value => { setCategory(value as string); setCreated(false) }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {categoriesForType.map(c => (
                    <SelectItem key={c.Slug} value={c.Slug}>
                      {c.Label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
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
                placeholder="0,00"
              />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label htmlFor="tx-date" className="mb-1.5 block text-xs font-medium text-muted-foreground">
                Vencimento
              </label>
              <Input
                id="tx-date"
                type="date"
                value={date}
                onChange={e => { setDate(e.target.value); setCreated(false) }}
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs font-medium text-muted-foreground">Status</label>
              <Select
                items={statusLabels}
                value={status}
                onValueChange={value => { setStatus(value as Status); setCreated(false) }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(statusLabels) as Status[]).map(key => (
                    <SelectItem key={key} value={key}>
                      {statusLabels[key]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
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
