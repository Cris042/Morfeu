import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiFalsa } from '../test/renderizar'
import { App } from './App'

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderizarEm(caminho: string) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={[caminho]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('shell', () => {
  it('mostra a marca (link para o cartaz), o cartaz e a atribuição do TMDB', async () => {
    apiFalsa({ '/api/filmes': { corpo: [] } })
    renderizarEm('/')
    expect(screen.getByRole('link', { name: 'Morfeu' })).toHaveAttribute('href', '/')
    expect(await screen.findByRole('heading', { name: 'Escolha o filme da noite' })).toBeInTheDocument()
    expect(screen.getByText(/fornecidos pelo TMDB/)).toBeInTheDocument()
  })

  it('sessão indisponível (404 do mapa) avisa e volta ao cartaz', async () => {
    apiFalsa({ '/api/sessoes/11/mapa': { status: 404, corpo: { erro: 'nao_encontrado' } }, '/api/holds': { corpo: [] } })
    renderizarEm('/sessoes/11')
    expect(await screen.findByRole('heading', { name: 'Esta sessão não está mais disponível' })).toBeInTheDocument()
  })

  it('rota desconhecida mostra o 404 amigável com volta ao cartaz', () => {
    renderizarEm('/nao/existe')
    expect(screen.getByRole('heading', { name: 'Essa sala não existe' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Voltar ao cartaz' })).toHaveAttribute('href', '/')
  })
})
