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
