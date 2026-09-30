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
