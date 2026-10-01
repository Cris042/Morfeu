import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from './client'

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
