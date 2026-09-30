import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { intervaloDoPolling } from '../../api/checkout'
import type { StatusPedido } from '../../api/tipos'
import { AcompanharPedido, TETO_POLLING_MS } from './AcompanharPedido'

const base = {
  id: 'p-1',
  codigo: 'ABCD2345EFGH6789',
  sessao_id: 7,
  assentos: ['C4'],
  total_centavos: 3200,
  expira_em: '2099-10-01T23:30:00Z',
}

// fetch que devolve os estados em sequência (o último se repete).
function servidor(estados: (StatusPedido | 404)[]) {
  let i = 0
  const f = vi.fn<typeof fetch>(() => {
    const e = estados[Math.min(i++, estados.length - 1)]
    if (e === 404) {
      return Promise.resolve(new Response('{"erro":"nao_encontrado"}', { status: 404 }))
    }
    return Promise.resolve(new Response(JSON.stringify({ ...base, status: e }), { status: 200 }))
  })
  vi.stubGlobal('fetch', f)
  return f
}

async function abrir() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={['/pedido/p-1']}>
        <Routes>
          <Route path="/pedido/:id" element={<AcompanharPedido />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  await avancar(0)
}

async function avancar(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('acompanhamento do pedido', () => {
  it('backoff do polling: 1 s, 1 s, 2 s, 4 s e teto de 5 s', () => {
    expect([0, 1, 2, 3, 4, 10].map(intervaloDoPolling)).toEqual([1000, 1000, 2000, 4000, 5000, 5000])
  })

  it('consulta até o webhook confirmar e para no estado terminal', async () => {
    const f = servidor(['aguardando_pagamento', 'aguardando_pagamento', 'pago'])
    await abrir()
    expect(screen.getByRole('heading', { name: 'Confirmando seu pagamento…' })).toBeInTheDocument()
    await avancar(1000)
    expect(f).toHaveBeenCalledTimes(2)
    await avancar(2000)
    await avancar(100)
    expect(f).toHaveBeenCalledTimes(3)
    expect(screen.getByRole('heading', { name: 'Pagamento confirmado' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Enviamos os ingressos')
    const chamadas = f.mock.calls.length
    await avancar(30_000)
    expect(f.mock.calls.length).toBe(chamadas)
  })

  it('teto: para de consultar, avisa do e-mail e "Verificar de novo" retoma', async () => {
    const f = servidor(['aguardando_pagamento'])
    await abrir()
    await avancar(TETO_POLLING_MS)
    expect(screen.getByRole('heading', { name: 'Estamos confirmando seu pagamento' })).toBeInTheDocument()
    const chamadas = f.mock.calls.length
    await avancar(60_000)
    expect(f.mock.calls.length).toBe(chamadas)
    await act(async () => {
      screen.getByRole('button', { name: 'Verificar de novo' }).click()
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(f.mock.calls.length).toBeGreaterThan(chamadas)
    expect(screen.getByRole('heading', { name: 'Confirmando seu pagamento…' })).toBeInTheDocument()
  })

  it.each([
    ['expirado', 'O prazo do pedido acabou'],
    ['falhou', 'O pagamento foi recusado'],
    ['estorno_pendente', 'Estorno em andamento'],
    ['estornado', 'Pagamento estornado'],
  ] as const)('%s → %s', async (status, titulo) => {
    servidor([status])
    await abrir()
    expect(screen.getByRole('heading', { name: titulo })).toBeInTheDocument()
  })

  it('pedido de outro navegador (404)', async () => {
    servidor([404])
    await abrir()
    expect(screen.getByRole('heading', { name: 'Pedido não encontrado neste navegador' })).toBeInTheDocument()
  })
})
