import { useQuery } from '@tanstack/react-query'

import { api } from './client'
import type { Pedido, StatusPedido } from './tipos'

// Checkout (PRD 0033). O client_secret só existe na memória do componente de
// pagamento: nunca em cache do TanStack Query, storage, URL ou log.

export interface PedidoCriado {
  pedido: Pedido
  client_secret: string
}

export interface Retomada {
  pedido_id: string
  codigo: string
  total_centavos: number
  expira_em: string
  client_secret: string
}

export function criarPedido(email: string, sessaoID: number, assentos: string[]) {
  return api.post<PedidoCriado>('/pedidos', { email, sessao_id: sessaoID, assentos })
}

export function retomarPedido(id: string) {
  return api.post<Retomada>(`/pedidos/${encodeURIComponent(id)}/retomar`)
}

/** Só existe com o gateway fake (dev/CI) — chamado pelo chunk de teste. */
export function pagarParaTeste(id: string) {
  return api.post<undefined>(`/__teste/pagar/${encodeURIComponent(id)}`)
}

/** Estados em que o polling para (estorno_pendente ainda vira estornado). */
export const TERMINAIS: ReadonlySet<StatusPedido> = new Set(['pago', 'expirado', 'falhou', 'estornado'])

/** Backoff do polling: 1 s, 2 s, 4 s e depois a cada 5 s. */
export function intervaloDoPolling(atualizacoes: number): number {
  return Math.min(1000 * 2 ** Math.max(0, atualizacoes - 1), 5000)
}

/** Acompanha o pedido até um estado terminal; pausa com a aba oculta. */
export function useAcompanharPedido(id: string, ativo: boolean) {
  return useQuery({
    queryKey: ['pedido', id],
    queryFn: () => api.get<Pedido>(`/pedidos/${encodeURIComponent(id)}`),
    refetchInterval: (q) => {
      const status = q.state.data?.status
      if (!ativo || (status && TERMINAIS.has(status))) {
        return false
      }
      return intervaloDoPolling(q.state.dataUpdateCount)
    },
    refetchIntervalInBackground: false,
  })
}
