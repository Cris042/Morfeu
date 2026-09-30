import { useQuery } from '@tanstack/react-query'

import { api } from './client'
import type { Pedido } from './tipos'

// Consulta de convidado e ingresso (PRD 0034/0035). A ref do ingresso
// ("{id}.{token}") é credencial: só em memória e no path, nunca em storage.

export interface IngressoDoPedido {
  assento: string
  ref: string
}

export interface ResultadoConsulta {
  pedido: Pedido
  ingressos: IngressoDoPedido[]
}

export function consultarPedido(email: string, codigo: string) {
  return api.post<ResultadoConsulta>('/pedidos/consulta', { email, codigo })
}

/** GET /i/{ref} — o ingresso válido (404 inválido; 410 expirado/cancelado). */
export interface Ingresso {
  assento: string
  status: 'ativo' | 'usado'
  filme: string
  sala: string
  inicio: string
}

export function useIngresso(ref: string) {
  return useQuery({
    queryKey: ['ingresso', ref],
    queryFn: () => api.get<Ingresso>(`/i/${encodeURIComponent(ref)}`),
    retry: false,
  })
}

/** Caminho do PNG do QR (mesma validação da página). */
export function caminhoDoQR(ref: string): string {
  return `/api/i/${encodeURIComponent(ref)}/qr.png`
}
