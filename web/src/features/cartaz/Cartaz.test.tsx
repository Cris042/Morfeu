import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiFalsa, renderizar } from '../../test/renderizar'
import { Cartaz } from './Cartaz'

afterEach(() => {
  vi.unstubAllGlobals()
})

const filmes = [
  { id: 1, titulo: 'Central do Brasil', ano: 1998, duracao_min: 113, poster_url: 'https://image.tmdb.org/t/p/w500/abc.jpg' },
  { id: 2, titulo: 'Cidade de Deus', ano: 2002, duracao_min: 130, poster_url: 'https://example.com/poster.jpg' },
]

describe('cartaz', () => {
  it('lista os filmes com link para o detalhe', async () => {
    apiFalsa({ '/api/filmes': { corpo: filmes } })
    renderizar(<Cartaz />)
    const link = await screen.findByRole('link', { name: /Central do Brasil/ })
    expect(link).toHaveAttribute('href', '/filmes/1')
    expect(screen.getByRole('heading', { level: 2, name: 'Cidade de Deus' })).toBeInTheDocument()
    expect(screen.getByText('1998 · 1h 53min')).toBeInTheDocument()
  })

  it('pôster: CDN do TMDB vira imagem; outro host vira pôster tipográfico', async () => {
    apiFalsa({ '/api/filmes': { corpo: filmes } })
    renderizar(<Cartaz />)
    const img = await screen.findByAltText('Pôster de Central do Brasil')
    expect(img.tagName).toBe('IMG')
    expect(img).toHaveAttribute('src', 'https://image.tmdb.org/t/p/w500/abc.jpg')
    const tipografico = screen.getByRole('img', { name: 'Pôster de Cidade de Deus' })
    expect(tipografico.tagName).toBe('DIV')
    expect(document.querySelector('img[src*="example.com"]')).toBeNull()
  })

  it('cartaz vazio tem mensagem própria', async () => {
    apiFalsa({ '/api/filmes': { corpo: [] } })
    renderizar(<Cartaz />)
    expect(await screen.findByText('Nenhum filme em cartaz agora. Volte mais tarde.')).toBeInTheDocument()
  })

  it('falha oferece tentar de novo e refaz a consulta', async () => {
    const f = apiFalsa({ '/api/filmes': { status: 500, corpo: { erro: 'erro_interno' } } })
    renderizar(<Cartaz />)
    const botao = await screen.findByRole('button', { name: 'Tentar de novo' })
    apiFalsa({ '/api/filmes': { corpo: filmes } })
    await userEvent.click(botao)
    expect(await screen.findByRole('link', { name: /Cidade de Deus/ })).toBeInTheDocument()
    expect(f).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(screen.queryByRole('alert')).toBeNull()
    })
  })
})
