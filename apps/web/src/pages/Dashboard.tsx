import { useEffect, useState } from 'react'
import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { api, formatBRL } from '../api/client'
import type { Entry, MonthlySummary, CategorySummary, CashFlowPoint } from '../api/client'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import KpiCard from '../components/KpiCard'
import CashFlowChart from '../components/CashFlowChart'
import IncomeExpenseChart from '../components/IncomeExpenseChart'
import CategoryDonut from '../components/CategoryDonut'
import TransactionsTable from '../components/TransactionsTable'

export default function Dashboard() {
  const userName = localStorage.getItem('user_name') ?? 'você'
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const monthLabel = format(now, 'MMMM yyyy', { locale: ptBR })
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')

  const [summary, setSummary] = useState<MonthlySummary | null>(null)
  const [categories, setCategories] = useState<CategorySummary[]>([])
  const [cashflow, setCashflow] = useState<CashFlowPoint[]>([])
  const [entries, setEntries] = useState<Entry[]>([])
  const [displayEntries, setDisplayEntries] = useState<Entry[]>([])
  const [loading, setLoading] = useState(true)
  const [monthlyData, setMonthlyData] = useState<{ month: string; income: number; expense: number }[]>([])

  useEffect(() => {
    loadAll()
  }, [])

  async function loadAll() {
    setLoading(true)
    try {
      const [s, cats, cf, ents] = await Promise.all([
        api.summary.monthly(currentMonth),
        api.summary.categories(),
        api.summary.cashflow(30),
        api.entries.list({ from: firstDay, to: lastDay }),
      ])
      setSummary(s)
      setCategories(cats.categories ?? [])
      setCashflow(cf.points ?? [])

      const all = ents.entries ?? []
      setEntries(all)
      setDisplayEntries(all.slice(0, 20))

      const months3 = [-2, -1, 0].map(offset => {
        const d = new Date(now.getFullYear(), now.getMonth() + offset, 1)
        return format(d, 'yyyy-MM')
      })
      const summaries = await Promise.all(months3.map(m => api.summary.monthly(m)))
      setMonthlyData(summaries.map((sm, i) => ({
        month: format(new Date(months3[i] + '-01'), 'MMM', { locale: ptBR }),
        income: sm.TotalIncome,
        expense: sm.TotalExpense,
      })))
    } catch (err) {
      console.error('load dashboard:', err)
    } finally {
      setLoading(false)
    }
  }

  async function handleMarkPaid(entryID: string) {
    try {
      await api.entries.update(entryID, { payment_status: 'paid' })
      await loadAll()
    } catch (err) {
      console.error('mark paid:', err)
    }
  }

  function handleLogout() {
    localStorage.clear()
    window.location.href = '/login'
  }

  const payableToday = entries
    .filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending' &&
      e.DueDate && e.DueDate.startsWith(format(new Date(), 'yyyy-MM-dd')))
    .reduce((sum, e) => sum + e.Amount, 0)

  const totalReceivable = entries
    .filter(e => e.Type === 'income' && e.PaymentStatus === 'pending')
    .reduce((sum, e) => sum + e.Amount, 0)

  const topExpenses = categories
    .filter(c => c.Type === 'expense')
    .sort((a, b) => b.Total - a.Total)
    .slice(0, 5)

  const worstMonth = monthlyData.length === 3
    ? monthlyData.reduce((prev, cur) =>
        prev.income - prev.expense < cur.income - cur.expense ? prev : cur
      )
    : null

  const expenseByDay: Record<string, number> = {}
  for (const e of entries) {
    if (e.Type === 'expense') {
      const day = e.Date.slice(0, 10)
      expenseByDay[day] = (expenseByDay[day] ?? 0) + e.Amount
    }
  }
  const worstDayEntry = Object.entries(expenseByDay).sort((a, b) => b[1] - a[1])[0]
  const worstDay = worstDayEntry
    ? { date: format(new Date(worstDayEntry[0]), 'dd/MM'), total: worstDayEntry[1] }
    : null

  const colorDots = ['#ef4444', '#f97316', '#f59e0b', '#8b5cf6', '#3b82f6']

  return (
    <div className="min-h-screen bg-background">
      <header className="bg-card border-b border-border px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className="text-2xl">💊</span>
          <div>
            <h1 className="font-bold text-foreground leading-tight">Farmácia — Painel Financeiro</h1>
            <p className="text-xs text-muted-foreground capitalize">{monthLabel}</p>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <span className="text-sm text-muted-foreground">Olá, <strong className="text-foreground">{userName}</strong></span>
          <button onClick={handleLogout} className="text-xs text-muted-foreground hover:text-foreground transition-colors">Sair</button>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-4 py-6 space-y-6">
        {loading ? (
          <div className="flex items-center justify-center h-64">
            <div className="text-muted-foreground text-sm animate-pulse">Carregando dados...</div>
          </div>
        ) : (
          <>
            <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
              <KpiCard title="Saldo do Mês" value={summary?.Balance ?? 0} icon="💰" color={(summary?.Balance ?? 0) >= 0 ? 'green' : 'red'} subtitle="Receitas − Despesas" />
              <KpiCard title="Total Receitas" value={summary?.TotalIncome ?? 0} icon="📈" color="green" subtitle="Este mês" />
              <KpiCard title="Total Despesas" value={summary?.TotalExpense ?? 0} icon="📉" color="red" subtitle="Este mês" />
              <KpiCard title="A Receber" value={totalReceivable} icon="⏳" color="blue" subtitle="Pendente" />
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <KpiCard title="A Pagar Hoje" value={payableToday} icon="⚠️" color="yellow" subtitle="Vencimento hoje" />
              {worstMonth && (
                <Card>
                  <CardContent className="p-5 flex items-center gap-4">
                    <span className="text-3xl">📉</span>
                    <div>
                      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Pior Mês</p>
                      <p className="text-sm text-foreground mt-1">
                        <strong className="capitalize">{worstMonth.month}</strong> — saldo de {formatBRL(worstMonth.income - worstMonth.expense)}
                      </p>
                    </div>
                  </CardContent>
                </Card>
              )}
              <Card>
                <CardContent className="p-5 flex items-center gap-4">
                  <span className="text-3xl">📱</span>
                  <div>
                    <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">WhatsApp Bot</p>
                    <p className="text-sm text-foreground mt-1">
                      Envie <Badge variant="secondary" className="font-mono text-xs">/despesa 500 aluguel</Badge> para registrar
                    </p>
                  </div>
                </CardContent>
              </Card>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
              <div className="lg:col-span-2"><CashFlowChart data={cashflow} /></div>
              <div><IncomeExpenseChart data={monthlyData} /></div>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
              <Card>
                <CardContent className="p-5">
                  <h3 className="text-sm font-semibold text-card-foreground mb-4">🔥 Maiores Gastos do Mês</h3>
                  {topExpenses.length === 0 ? (
                    <p className="text-muted-foreground text-sm text-center py-6">Sem dados</p>
                  ) : (
                    <div className="space-y-3">
                      {topExpenses.map((cat, i) => (
                        <div key={cat.Category} className="flex items-center gap-3">
                          <span className="w-2.5 h-2.5 rounded-full flex-shrink-0" style={{ backgroundColor: colorDots[i] }} />
                          <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium text-foreground truncate">{cat.Category.replace(/_/g, ' ')}</p>
                            <p className="text-xs text-muted-foreground">{cat.Count} registro(s)</p>
                          </div>
                          <span className="text-sm font-semibold text-foreground">{formatBRL(cat.Total)}</span>
                        </div>
                      ))}
                    </div>
                  )}
                  {worstDay && (
                    <div className="mt-4 pt-4 border-t border-border">
                      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Pior dia do mês</p>
                      <div className="flex items-center justify-between mt-1">
                        <span className="text-sm text-foreground">{worstDay.date}</span>
                        <span className="text-sm font-semibold text-destructive">{formatBRL(worstDay.total)}</span>
                      </div>
                    </div>
                  )}
                </CardContent>
              </Card>
              <div><CategoryDonut data={categories} /></div>
              <div className="lg:col-span-1">
                <TransactionsTable entries={displayEntries} onMarkPaid={handleMarkPaid} />
              </div>
            </div>
          </>
        )}
      </main>
    </div>
  )
}
