import { useQuery } from '@tanstack/react-query'

import { api, ErroApi } from './client'
import type { Filme, SessaoPublica } from './tipos'

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
