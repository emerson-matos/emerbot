import { QueryCache, QueryClient } from '@tanstack/react-query'
import { notifyError } from '@/lib/toast'

// PT-BR read-failure message per resource; keeps backend error strings (in
// English) out of the UI. Mutation success/failure messages are handled
// per-mutation instead, since those are inherently action-specific.
const resourceMessages: Record<string, string> = {
  summary: 'Não foi possível carregar o resumo financeiro.',
  entries: 'Não foi possível carregar as transações.',
  goal: 'Não foi possível carregar a meta do mês.',
}

export const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (_error, query) => {
      const resource = typeof query.queryKey[0] === 'string' ? query.queryKey[0] : ''
      notifyError(resourceMessages[resource] ?? 'Não foi possível carregar os dados. Verifique sua conexão.')
    },
  }),
})
