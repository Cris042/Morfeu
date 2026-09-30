import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'

import { App } from './App'

function renderizarEm(caminho: string) {
  render(
    <MemoryRouter initialEntries={[caminho]}>
      <App />
    </MemoryRouter>,
  )
}

describe('shell', () => {
  it('mostra a marca, o cartaz e a atribuição do TMDB', () => {
    renderizarEm('/')
    expect(screen.getByText('Morfeu')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Em cartaz' })).toBeInTheDocument()
    expect(screen.getByText(/fornecidos pelo TMDB/)).toBeInTheDocument()
  })

  it('rota desconhecida mostra o 404 amigável com volta ao cartaz', () => {
    renderizarEm('/nao/existe')
    expect(screen.getByRole('heading', { name: 'Essa sala não existe' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Voltar ao cartaz' })).toHaveAttribute('href', '/')
  })
})
