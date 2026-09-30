import { keepPreviousData, useQuery } from '@tanstack/react-query'

import { api } from './client'
import { useSessao } from './sessao'
import type { Pedido } from './tipos'

// "Meus pedidos" (PRD 0032): a chave leva o id da conta — trocar de conta
// nunca mostra o cache da anterior — e só consulta logado.
function useContaID(): string | undefined {
  const sessao = useSessao()
  return sessao.status === 'logado' ? sessao.usuario.id : undefined
}

export function useMeusPedidos(pagina: number) {
  const conta = useContaID()
  return useQuery({
    queryKey: ['conta', conta, 'pedidos', pagina],
    queryFn: () => api.get<{ pedidos: Pedido[] }>(`/pedidos?pagina=${String(pagina)}`),
    enabled: conta !== undefined,
    placeholderData: keepPreviousData,
  })
}

export function usePedidoDaConta(id: string) {
  const conta = useContaID()
  return useQuery({
    queryKey: ['conta', conta, 'pedido', id],
    queryFn: () => api.get<Pedido>(`/pedidos/${encodeURIComponent(id)}`),
    enabled: conta !== undefined,
  })
}

/** Tamanho da página no servidor (pedido.TamanhoPaginaPedidos). */
export const PEDIDOS_POR_PAGINA = 20
