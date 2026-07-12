import { formatBRL } from '../api/client'
import type { Entry } from '../api/client'
import { format, parseISO } from 'date-fns'
import { ptBR } from 'date-fns/locale'

interface Props {
  entries: Entry[]
  onMarkPaid?: (id: string) => void
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

export default function TransactionsTable({ entries, onMarkPaid }: Props) {
  return (
    <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
      <div className="px-5 py-4 border-b border-gray-100">
        <h3 className="text-sm font-semibold text-gray-700">🧾 Últimas Transações</h3>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="bg-gray-50 text-xs text-gray-500 uppercase tracking-wide">
              <th className="px-4 py-3 text-left">Data</th>
              <th className="px-4 py-3 text-left">Descrição</th>
              <th className="px-4 py-3 text-left">Categoria</th>
              <th className="px-4 py-3 text-right">Valor</th>
              <th className="px-4 py-3 text-center">Status</th>
              {onMarkPaid && <th className="px-4 py-3" />}
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-50">
            {entries.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-gray-400">
                  Nenhuma transação encontrada
                </td>
              </tr>
            )}
            {entries.map(e => (
              <tr key={e.EntryID} className="hover:bg-gray-50 transition-colors">
                <td className="px-4 py-3 text-gray-500 whitespace-nowrap">
                  {format(parseISO(e.Date), 'dd/MM/yy', { locale: ptBR })}
                </td>
                <td className="px-4 py-3 text-gray-900 max-w-xs truncate">
                  {e.Description || '—'}
                </td>
                <td className="px-4 py-3">
                  <span className="inline-block bg-gray-100 text-gray-600 text-xs rounded-full px-2 py-0.5">
                    {categoryLabels[e.Category] ?? e.Category}
                  </span>
                </td>
                <td className={`px-4 py-3 text-right font-medium tabular-nums ${e.Type === 'income' ? 'text-emerald-600' : 'text-red-600'}`}>
                  {e.Type === 'income' ? '+' : '-'}{formatBRL(e.Amount)}
                </td>
                <td className="px-4 py-3 text-center">
                  {e.PaymentStatus === 'paid' ? (
                    <span className="text-xs bg-emerald-100 text-emerald-700 rounded-full px-2 py-0.5">Pago</span>
                  ) : (
                    <span className="text-xs bg-amber-100 text-amber-700 rounded-full px-2 py-0.5">Pendente</span>
                  )}
                </td>
                {onMarkPaid && (
                  <td className="px-4 py-3 text-center">
                    {e.PaymentStatus === 'pending' && (
                      <button
                        onClick={() => onMarkPaid(e.EntryID)}
                        className="text-xs text-emerald-600 hover:underline"
                      >
                        Marcar pago
                      </button>
                    )}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
