import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { vi } from 'vitest'

// Renderiza com QueryClient novo (sem cache entre testes e sem retry) e rota.
export function renderizar(ui: ReactElement, { caminho = '/', padrao = '*' } = {}) {
  const cliente = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={cliente}>
      <MemoryRouter initialEntries={[caminho]}>
        <Routes>
          <Route path={padrao} element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

type Rotas = Record<string, { status?: number; corpo?: unknown }>

// Dublê de fetch por caminho da API (sem rede). Caminho ausente → 500.
export function apiFalsa(rotas: Rotas) {
  const fetchFalso = vi.fn<typeof fetch>((entrada) => {
    const url = typeof entrada === 'string' ? entrada : entrada instanceof URL ? entrada.pathname : entrada.url
    const r = rotas[url] ?? { status: 500, corpo: { erro: 'erro_interno' } }
    return Promise.resolve(new Response(r.corpo === undefined ? null : JSON.stringify(r.corpo), { status: r.status ?? 200 }))
  })
  vi.stubGlobal('fetch', fetchFalso)
  return fetchFalso
}
