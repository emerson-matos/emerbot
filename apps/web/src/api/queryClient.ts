import { QueryCache, QueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ApiError, NetworkError, ForbiddenError, UnauthorizedError } from './api-error'

const resourceMessages: Record<string, string> = {
  summary: 'Não foi possível carregar o resumo financeiro.',
  entries: 'Não foi possível carregar as transações.',
  goal: 'Não foi possível carregar a meta do mês.',
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry(failureCount, error) {
        if (error instanceof NetworkError) return false
        if (error instanceof UnauthorizedError) return false
        if (error instanceof ForbiddenError) return false
        if (error instanceof ApiError && error.status < 500) return false
        return failureCount < 3
      },
    },
  },
  queryCache: new QueryCache({
    onError: (error, query) => {
      if (error instanceof UnauthorizedError) return
      if (error instanceof ForbiddenError) {
        toast.error('Acesso negado. Você não tem permissão para acessar este recurso.')
        return
      }
      if (error instanceof NetworkError) {
        toast.error('Erro de conexão. Verifique sua internet.')
        return
      }
      const resource = typeof query.queryKey[0] === 'string' ? query.queryKey[0] : ''
      toast.error(resourceMessages[resource] ?? 'Não foi possível carregar os dados. Verifique sua conexão.')
    },
  }),
})
