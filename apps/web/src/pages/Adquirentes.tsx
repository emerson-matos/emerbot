import { format } from 'date-fns'
import { usePaymentsSales } from '../api/queries'
import { SalesCard, ReceivablesCard, ForecastCard } from '../components/PaymentsCards'
import { Card, CardContent } from '@/components/ui/card'
import { formatBRL } from '@/lib/format'

const methodLabels: Record<string, string> = {
  credito: 'Crédito',
  debito: 'Débito',
  pix: 'Pix',
  boleto: 'Boleto',
  outros: 'Outros',
}

export default function Adquirentes() {
  const now = new Date()
  const firstDay = format(new Date(now.getFullYear(), now.getMonth(), 1), 'yyyy-MM-dd')
  const lastDay = format(new Date(now.getFullYear(), now.getMonth() + 1, 0), 'yyyy-MM-dd')

  const salesQuery = usePaymentsSales(firstDay, lastDay)
  const byMethod = salesQuery.data?.by_method ?? {}
  const methods = Object.entries(byMethod).sort((a, b) => b[1] - a[1])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Adquirentes</h1>
        <p className="mt-1 text-muted-foreground">
          Vendas, recebíveis e projeção de caixa importados das maquininhas (PagBank).
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <SalesCard />
        <ReceivablesCard />
        <ForecastCard />
      </div>

      <Card>
        <CardContent className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">
            Vendas por meio de pagamento · este mês
          </h2>
          {methods.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Sem vendas importadas neste mês.
            </p>
          ) : (
            <ul className="divide-y divide-border">
              {methods.map(([method, value]) => (
                <li key={method} className="flex items-center justify-between py-2 text-sm">
                  <span>{methodLabels[method] ?? method}</span>
                  <span className="font-medium tabular-nums">{formatBRL(value)}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
