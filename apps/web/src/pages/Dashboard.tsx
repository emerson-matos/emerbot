import { useEffect, useState } from 'react'
import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import {
  api, Entry, MonthlySummary, CategorySummary, CashFlowPoint, formatBRL,
} from '../api/client'
import KpiCard from '../components/KpiCard'
import CashFlowChart from '../components/CashFlowChart'
import IncomeExpenseChart from '../components/IncomeExpenseChart'
import CategoryDonut from '../components/CategoryDonut'
import TransactionsTable from '../components/TransactionsTable'

export default function Dashboard() {
  const userName = localStorage.getItem('user_name') ?? 'você'
  const currentMonth = format(new Date(), 'yyyy-MM')
  const monthLabel = format(new Date(), 'MMMM yyyy', { locale: ptBR })

  const [summary, setSummary] = useState<MonthlySummary | null>(null)
  const [categories, setCategories] = useState<CategorySummary[]>([])
  const [cashflow, setCashflow] = useState<CashFlowPoint[]>([])
  const [entries, setEntries] = useState<Entry[]>([])
  const [loading, setLoading] = useState(true)

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
        api.entries.list({ from: format(new Date(new Date().setDate(1)), 'yyyy-MM-dd') }),
      ])
      setSummary(s)
      setCategories(cats.categories ?? [])
      setCashflow(cf.points ?? [])
      setEntries((ents.entries ?? []).slice(0, 20))
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

  // KPIs derived from entries
  const payableToday = entries
    .filter(e => e.Type === 'expense' && e.PaymentStatus === 'pending' &&
      e.DueDate && e.DueDate.startsWith(format(new Date(), 'yyyy-MM-dd')))
    .reduce((sum, e) => sum + e.Amount, 0)

  const totalReceivable = entries
    .filter(e => e.Type === 'income' && e.PaymentStatus === 'pending')
    .reduce((sum, e) => sum + e.Amount, 0)

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <header className="bg-white border-b border-gray-200 px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className="text-2xl">💊</span>
          <div>
            <h1 className="font-bold text-gray-900 leading-tight">Farmácia — Painel Financeiro</h1>
            <p className="text-xs text-gray-500 capitalize">{monthLabel}</p>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <span className="text-sm text-gray-600">Olá, <strong>{userName}</strong></span>
          <button
            onClick={handleLogout}
            className="text-xs text-gray-400 hover:text-gray-700 transition-colors"
          >
            Sair
          </button>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-4 py-6 space-y-6">
        {loading ? (
          <div className="flex items-center justify-center h-64">
            <div className="text-gray-400 text-sm animate-pulse">Carregando dados...</div>
          </div>
        ) : (
          <>
            {/* KPI Cards */}
            <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
              <KpiCard
                title="Saldo do Mês"
                value={summary?.Balance ?? 0}
                icon="💰"
                color={( summary?.Balance ?? 0) >= 0 ? 'green' : 'red'}
                subtitle={`Receitas − Despesas`}
              />
              <KpiCard
                title="Total Receitas"
                value={summary?.TotalIncome ?? 0}
                icon="📈"
                color="green"
                subtitle="Este mês"
              />
              <KpiCard
                title="Total Despesas"
                value={summary?.TotalExpense ?? 0}
                icon="📉"
                color="red"
                subtitle="Este mês"
              />
              <KpiCard
                title="A Receber"
                value={totalReceivable}
                icon="⏳"
                color="blue"
                subtitle="Pendente"
              />
            </div>

            {/* Second row KPIs */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <KpiCard
                title="A Pagar Hoje"
                value={payableToday}
                icon="⚠️"
                color="yellow"
                subtitle="Vencimento hoje"
              />
              <div className="bg-white rounded-xl border border-gray-200 p-5 flex items-center gap-4">
                <span className="text-3xl">📱</span>
                <div>
                  <p className="text-xs font-medium text-gray-500 uppercase tracking-wide">WhatsApp Bot</p>
                  <p className="text-sm text-gray-700 mt-1">
                    Envie <code className="bg-gray-100 px-1 rounded">/despesa 500 aluguel</code> para registrar
                  </p>
                </div>
              </div>
            </div>

            {/* Charts row */}
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
              <div className="lg:col-span-2">
                <CashFlowChart data={cashflow} />
              </div>
              <div>
                <IncomeExpenseChart
                  income={summary?.TotalIncome ?? 0}
                  expense={summary?.TotalExpense ?? 0}
                  month={monthLabel}
                />
              </div>
            </div>

            {/* Category donut + table */}
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
              <div>
                <CategoryDonut data={categories} />
              </div>
              <div className="lg:col-span-2">
                <TransactionsTable
                  entries={entries}
                  onMarkPaid={handleMarkPaid}
                />
              </div>
            </div>
          </>
        )}
      </main>
    </div>
  )
}
