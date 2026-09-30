import { screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiFalsa, renderizar } from '../../test/renderizar'
import { PaginaFilme } from './PaginaFilme'

afterEach(() => {
  vi.unstubAllGlobals()
})

const filme = { id: 7, titulo: 'O Auto da Compadecida', ano: 2000, duracao_min: 104, sinopse: 'João Grilo e Chicó.' }
const sessoes = [
  { id: 11, sala_id: 1, sala_nome: 'Sala 1', inicio: '2099-10-01T23:30:00Z', fim: '2099-10-02T01:34:00Z', preco_centavos: 3200 },
  // 02:00 UTC do dia 2 = 23:00 do dia 1 em São Paulo: mesmo grupo.
  { id: 12, sala_id: 2, sala_nome: 'Sala 2', inicio: '2099-10-02T02:00:00Z', fim: '2099-10-02T04:04:00Z', preco_centavos: 2800 },
  { id: 13, sala_id: 1, sala_nome: 'Sala 1', inicio: '2099-10-02T17:00:00Z', fim: '2099-10-02T19:04:00Z', preco_centavos: 3200 },
]

function abrir(caminho: string) {
  renderizar(<PaginaFilme />, { caminho, padrao: '/filmes/:id' })
}

describe('página do filme', () => {
  it('mostra o filme e as sessões por dia, no fuso do cinema e em reais', async () => {
    apiFalsa({ '/api/filmes/7': { corpo: filme }, '/api/filmes/7/sessoes': { corpo: sessoes } })
    abrir('/filmes/7')
    expect(await screen.findByRole('heading', { level: 1, name: 'O Auto da Compadecida' })).toBeInTheDocument()
    expect(screen.getByText('João Grilo e Chicó.')).toBeInTheDocument()

    const primeira = await screen.findByRole('link', { name: 'Sessão das 20:30, Sala 1, R$ 32,00' })
    expect(primeira).toHaveAttribute('href', '/sessoes/11')
    expect(screen.getByRole('link', { name: 'Sessão das 23:00, Sala 2, R$ 28,00' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Sessão das 14:00, Sala 1, R$ 32,00' })).toBeInTheDocument()

    const dias = screen.getAllByRole('heading', { level: 3 })
    expect(dias).toHaveLength(2)
    expect(dias[0]).toHaveTextContent(/1 de out/)
    expect(dias[1]).toHaveTextContent(/2 de out/)
  })

  it('sem sessões avisa', async () => {
    apiFalsa({ '/api/filmes/7': { corpo: filme }, '/api/filmes/7/sessoes': { corpo: [] } })
    abrir('/filmes/7')
    expect(await screen.findByText('Sem sessões programadas para este filme.')).toBeInTheDocument()
  })

  it('filme inexistente ou arquivado (404) sai do cartaz', async () => {
    apiFalsa({ '/api/filmes/9': { status: 404, corpo: { erro: 'nao_encontrado' } }, '/api/filmes/9/sessoes': { corpo: [] } })
    abrir('/filmes/9')
    expect(await screen.findByRole('heading', { name: 'Este filme não está em cartaz' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Ver o cartaz' })).toHaveAttribute('href', '/')
  })

  it('id inválido nem chama a API', () => {
    const f = apiFalsa({})
    abrir('/filmes/abc')
    expect(screen.getByRole('heading', { name: 'Este filme não está em cartaz' })).toBeInTheDocument()
    expect(f).not.toHaveBeenCalled()
  })

  it('sinopse com HTML aparece como texto', async () => {
    const hostil = { ...filme, sinopse: '<img src=x onerror=alert(1)><script>alert(2)</script>' }
    apiFalsa({ '/api/filmes/7': { corpo: hostil }, '/api/filmes/7/sessoes': { corpo: [] } })
    abrir('/filmes/7')
    expect(await screen.findByText(hostil.sinopse)).toBeInTheDocument()
    expect(document.querySelector('script')).toBeNull()
    expect(document.querySelector('img[src="x"]')).toBeNull()
  })
})
