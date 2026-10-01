import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from './client'
import type { StatusPedido } from './tipos'

// Backoffice do operador (PRD 0038). A autorização é da API (RBAC em toda
// rota /backoffice/*); o guarda de papel da SPA é só experiência.

/** Filme como o backoffice o vê (inclui arquivados). */
export interface FilmeBackoffice {
  id: number
  titulo: string
  ano?: number
  duracao_min?: number
  tmdb_id?: number
  arquivado_em?: string
}

/** Resultado da busca no TMDB (dados externos — exibidos só como texto). */
export interface ResultadoTMDB {
  tmdb_id: number
  titulo: string
  ano?: number
}

export interface Posicao {
  fileira: string
  coluna: number
}

export interface Layout {
  fileiras: number
  colunas: number
  vaos: Posicao[] | null
  pcd: Posicao[] | null
}

export interface Sala {
  id: number
  nome: string
  layout: Layout
}

const FILMES = ['backoffice', 'filmes']
const SALAS = ['backoffice', 'salas']

export function useFilmesBackoffice() {
  return useQuery({ queryKey: FILMES, queryFn: () => api.get<FilmeBackoffice[]>('/backoffice/filmes') })
}

export function buscarNoTMDB(q: string) {
  return api.get<{ resultados: ResultadoTMDB[] }>(`/backoffice/filmes/tmdb?q=${encodeURIComponent(q)}`)
}

export function useImportarFilme() {
  const cliente = useQueryClient()
  return useMutation({
    mutationFn: (tmdbID: number) => api.post<{ filme: FilmeBackoffice; criado: boolean }>('/backoffice/filmes/importar', { tmdb_id: tmdbID }),
    onSuccess: () => cliente.invalidateQueries({ queryKey: FILMES }),
  })
}

export function useArquivarFilme() {
  const cliente = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.post<undefined>(`/backoffice/filmes/${String(id)}/arquivar`),
    onSuccess: () => cliente.invalidateQueries({ queryKey: FILMES }),
  })
}

export function useSalas() {
  return useQuery({ queryKey: SALAS, queryFn: () => api.get<Sala[]>('/backoffice/salas') })
}

/** Cria (sem id) ou atualiza (com id) uma sala; o layout é validado no servidor. */
export function useSalvarSala() {
  const cliente = useQueryClient()
  return useMutation({
    mutationFn: ({ id, nome, layout }: { id?: number; nome: string; layout: unknown }) =>
      id === undefined
        ? api.post<Sala>('/backoffice/salas', { nome, layout })
        : api.put<Sala>(`/backoffice/salas/${String(id)}`, { nome, layout }),
    onSuccess: () => cliente.invalidateQueries({ queryKey: SALAS }),
  })
}

/** Sessão como o backoffice a vê. */
export interface SessaoBackoffice {
  id: number
  filme_id: number
  sala_id: number
  inicio: string
  fim: string
  duracao_min: number
  preco_centavos: number
  status: 'agendada' | 'cancelada'
}

const SESSOES = ['backoffice', 'sessoes']
const PEDIDOS = ['backoffice', 'pedidos']

export function useSessoesBackoffice() {
  return useQuery({ queryKey: SESSOES, queryFn: () => api.get<SessaoBackoffice[]>('/backoffice/sessoes') })
}

export function useCriarSessao() {
  const cliente = useQueryClient()
  return useMutation({
    mutationFn: (s: { filme_id: number; sala_id: number; inicio: string; preco_centavos: number }) =>
      api.post<SessaoBackoffice>('/backoffice/sessoes', s),
    onSuccess: () => cliente.invalidateQueries({ queryKey: SESSOES }),
  })
}

export function useCancelarSessao() {
  const cliente = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.post<{ pedidos_estornados: number }>(`/backoffice/sessoes/${String(id)}/cancelar`),
    onSuccess: () => Promise.all([cliente.invalidateQueries({ queryKey: SESSOES }), cliente.invalidateQueries({ queryKey: PEDIDOS })]),
  })
}

/** Pedido como o operador o vê: e-mail mascarado, nunca o código (PRD 0037). */
export interface PedidoOperador {
  id: string
  sessao_id: number
  email: string
  assentos: string[]
  total_centavos: number
  status: StatusPedido
  motivo_estorno?: string
  criado_em: string
}

export interface DetalheOperador {
  pedido: PedidoOperador
  ingressos: { assento: string; status: string }[]
  eventos: { de: string; para: string; ocorrido_em: string }[]
}

/** Página do servidor (pedido.TamanhoPaginaOperador). */
export const PEDIDOS_OPERADOR_POR_PAGINA = 50

export interface FiltroPedidos {
  sessao?: number
  status?: StatusPedido
}

function consultaDe(f: FiltroPedidos, pagina: number): string {
  const p = new URLSearchParams()
  if (f.sessao !== undefined) {
    p.set('sessao_id', String(f.sessao))
  }
  if (f.status) {
    p.set('status', f.status)
  }
  p.set('pagina', String(pagina))
  return p.toString()
}

export function listarPedidosOperador(f: FiltroPedidos, pagina = 1) {
  return api.get<{ pedidos: PedidoOperador[] }>(`/backoffice/pedidos?${consultaDe(f, pagina)}`)
}

export function usePedidosOperador(f: FiltroPedidos, pagina: number) {
  return useQuery({ queryKey: [...PEDIDOS, f, pagina], queryFn: () => listarPedidosOperador(f, pagina) })
}

export function usePedidoOperador(id: string) {
  return useQuery({ queryKey: [...PEDIDOS, 'detalhe', id], queryFn: () => api.get<DetalheOperador>(`/backoffice/pedidos/${encodeURIComponent(id)}`) })
}

export function useCancelarPedidoOperador(id: string) {
  const cliente = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<DetalheOperador>(`/backoffice/pedidos/${encodeURIComponent(id)}/cancelar`),
    onSuccess: () => cliente.invalidateQueries({ queryKey: PEDIDOS }),
  })
}
