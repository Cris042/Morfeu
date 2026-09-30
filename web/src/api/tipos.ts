// Espelho do contrato público da API (catalogo e sessao). Instantes em ISO UTC.

export interface Filme {
  id: number
  titulo: string
  sinopse?: string
  duracao_min?: number
  ano?: number
  poster_url?: string
}

export interface SessaoPublica {
  id: number
  sala_id: number
  sala_nome: string
  inicio: string
  fim: string
  preco_centavos: number
}

/** Cadeira real da sala (vãos não geram assento). */
export interface Assento {
  codigo: string
  fileira: string
  coluna: number
  pcd: boolean
}

/** GET /sessoes/{id}/mapa — layout da sala da sessão. */
export interface MapaSessao {
  sessao_id: number
  sala_id: number
  sala_nome: string
  fileiras: number
  colunas: number
  assentos: Assento[]
}

/** GET /sessoes/{id}/ocupacao — só os códigos ocupados (sem dono). */
export interface Ocupacao {
  sessao_id: number
  ocupados: string[]
}

/** Hold do carrinho (cookie HttpOnly): trava de um assento por 10 min. */
export interface Hold {
  id: string
  sessao_id: number
  assento: string
  expira_em: string
  extensoes_usadas: number
}

/** GET /auth/eu — dono da sessão (PRD 0032). */
export interface Usuario {
  id: string
  nome: string
  email: string
  papel: 'cliente' | 'operador'
}

export type StatusPedido = 'aguardando_pagamento' | 'pago' | 'expirado' | 'falhou' | 'estorno_pendente' | 'estornado'

/** Pedido como a API o devolve (GET /pedidos, GET /pedidos/{id}). */
export interface Pedido {
  id: string
  codigo: string
  sessao_id: number
  assentos: string[]
  total_centavos: number
  status: StatusPedido
  expira_em: string
}
