import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, ErroApi } from './client'
import type { Filme, Hold, MapaSessao, Ocupacao, SessaoPublica } from './tipos'

// Só repete falha de rede/5xx: 4xx (ex.: 404 de filme arquivado) é resposta.
// Política global do QueryClient (main.tsx) — os testes usam retry: false.
export function repetirSeTransitorio(tentativas: number, erro: Error): boolean {
  if (erro instanceof ErroApi && erro.status >= 400 && erro.status < 500) {
    return false
  }
  return tentativas < 1
}

export function useFilmes() {
  return useQuery({
    queryKey: ['filmes'],
    queryFn: () => api.get<Filme[]>('/filmes'),
  })
}

export function useFilme(id: number) {
  return useQuery({
    queryKey: ['filme', id],
    queryFn: () => api.get<Filme>(`/filmes/${String(id)}`),
  })
}

export function useSessoesDoFilme(id: number) {
  return useQuery({
    queryKey: ['sessoes-do-filme', id],
    queryFn: () => api.get<SessaoPublica[]>(`/filmes/${String(id)}/sessoes`),
  })
}

export function naoEncontrado(erro: unknown): boolean {
  return erro instanceof ErroApi && erro.status === 404
}

// Polling da ocupação (refinamento E5: 3–5 s; cache do servidor de 3 s, ADR 0008).
export const INTERVALO_OCUPACAO_MS = 4000

export function useMapa(sessaoID: number) {
  return useQuery({
    queryKey: ['mapa', sessaoID],
    queryFn: () => api.get<MapaSessao>(`/sessoes/${String(sessaoID)}/mapa`),
    staleTime: Infinity, // o layout da sala não muda durante a escolha
  })
}

export function useOcupacao(sessaoID: number, ativa = true) {
  return useQuery({
    queryKey: ['ocupacao', sessaoID],
    queryFn: () => api.get<Ocupacao>(`/sessoes/${String(sessaoID)}/ocupacao`),
    enabled: ativa,
    refetchInterval: INTERVALO_OCUPACAO_MS,
    refetchIntervalInBackground: false, // aba sem foco não consulta
  })
}

export function useMeusHolds() {
  return useQuery({
    queryKey: ['holds'],
    queryFn: () => api.get<Hold[]>('/holds'),
  })
}

// Toda escrita termina reconsultando os holds e a ocupação — inclusive no
// erro: o 409 da trava é a verdade e o mapa se atualiza na hora (RF03).
function useRecarregarReserva(sessaoID: number) {
  const cliente = useQueryClient()
  return () =>
    Promise.all([
      cliente.invalidateQueries({ queryKey: ['holds'] }),
      cliente.invalidateQueries({ queryKey: ['ocupacao', sessaoID] }),
    ])
}

export function useTravar(sessaoID: number) {
  const recarregar = useRecarregarReserva(sessaoID)
  return useMutation({
    mutationFn: (assentos: string[]) =>
      api.post<{ holds: Hold[] }>(`/sessoes/${String(sessaoID)}/holds`, { assentos }),
    onSettled: recarregar,
  })
}

export function useEstender(sessaoID: number) {
  const recarregar = useRecarregarReserva(sessaoID)
  return useMutation({
    mutationFn: (holdID: string) => api.post<Hold>(`/holds/${holdID}/estender`),
    onSettled: recarregar,
  })
}

export function useLiberar(sessaoID: number) {
  const recarregar = useRecarregarReserva(sessaoID)
  return useMutation({
    mutationFn: (holdID: string) => api.delete(`/holds/${holdID}`),
    onSettled: recarregar,
  })
}
