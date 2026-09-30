import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { iniciarSessao } from '../../api/sessao'
import { App } from '../../app/App'
import { apiFalsa } from '../../test/renderizar'

const futuro = '2099-10-01T23:40:00Z'
const holds = [
  { id: 'h2', sessao_id: 7, assento: 'C5', expira_em: futuro, extensoes_usadas: 0 },
  { id: 'h1', sessao_id: 7, assento: 'C4', expira_em: futuro, extensoes_usadas: 0 },
  { id: 'h3', sessao_id: 8, assento: 'A1', expira_em: futuro, extensoes_usadas: 0 },
]
const pedido = {
  id: 'p-1',
  codigo: 'ABCD2345EFGH6789',
  sessao_id: 7,
  assentos: ['C4', 'C5'],
  total_centavos: 6400,
  status: 'aguardando_pagamento',
  expira_em: '2099-10-01T23:30:00Z',
}
const ana = { id: 'u1', nome: 'Ana', email: 'ana@exemplo.com', papel: 'cliente' }

afterEach(() => {
  vi.unstubAllGlobals()
})

async function visitante(rotas: Parameters<typeof apiFalsa>[0]) {
  const f = apiFalsa({ '/api/auth/refresh': { status: 401 }, '/api/holds': { corpo: holds }, ...rotas })
  await iniciarSessao()
  return f
}

function abrir() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={['/sessoes/7/pagamento']}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function pedirComEmail(email = 'bia@exemplo.com') {
  const campo = await screen.findByLabelText('E-mail para receber os ingressos')
  await userEvent.clear(campo)
  await userEvent.type(campo, email)
  await userEvent.click(screen.getByRole('button', { name: 'Ir para o pagamento' }))
}

function corpoDe(f: ReturnType<typeof apiFalsa>, url: string): unknown {
  const init = f.mock.calls.find(([u]) => u === url)?.[1]
  return typeof init?.body === 'string' ? JSON.parse(init.body) : undefined
}

describe('checkout', () => {
  it('convidado: só os assentos desta sessão, pedido criado e pagamento aberto sem vazar o segredo', async () => {
    const f = await visitante({ '/api/pedidos': { status: 201, corpo: { pedido, client_secret: 'pi_x_secret_y' } } })
    abrir()
    expect(await screen.findByText('C4, C5')).toBeInTheDocument()
    expect(screen.getByText(/Compra como convidado/)).toBeInTheDocument()
    await pedirComEmail()
    expect(corpoDe(f, '/api/pedidos')).toEqual({ email: 'bia@exemplo.com', sessao_id: 7, assentos: ['C4', 'C5'] })
    expect(await screen.findByRole('heading', { name: 'Pagamento' })).toBeInTheDocument()
    expect(screen.getByText('ABCD2345EFGH6789')).toBeInTheDocument()
    expect(screen.getByText('R$ 64,00')).toBeInTheDocument()
    // Sem VITE_STRIPE_PK no teste: o meio avisa em vez de quebrar.
    expect(await screen.findByText(/pagamento com cartão não está disponível/)).toBeInTheDocument()
    expect(window.localStorage.length + window.sessionStorage.length).toBe(0)
    expect(document.body.innerHTML).not.toContain('pi_x_secret_y')
  })

  it('logado: e-mail da conta pré-preenchido e o pedido leva o Bearer', async () => {
    const f = apiFalsa({
      '/api/auth/refresh': { corpo: { access_token: 'tok', expira_em: 600 } },
      '/api/auth/eu': { corpo: ana },
      '/api/holds': { corpo: holds },
      '/api/pedidos': { status: 201, corpo: { pedido, client_secret: 's' } },
    })
    await iniciarSessao()
    abrir()
    expect(await screen.findByLabelText('E-mail para receber os ingressos')).toHaveValue('ana@exemplo.com')
    await userEvent.click(screen.getByRole('button', { name: 'Ir para o pagamento' }))
    await screen.findByRole('heading', { name: 'Pagamento' })
    const init = f.mock.calls.find(([u]) => u === '/api/pedidos')?.[1]
    expect((init?.headers as Record<string, string>).Authorization).toBe('Bearer tok')
  })

  it('pedido pendente no carrinho: retoma o pagamento dele', async () => {
    const f = await visitante({
      '/api/pedidos': { status: 409, corpo: { erro: 'pedido_pendente', pedido_id: 'p-9' } },
      '/api/pedidos/p-9/retomar': {
        corpo: { pedido_id: 'p-9', codigo: 'PENDENTE12345678', total_centavos: 3200, expira_em: futuro, client_secret: 's' },
      },
    })
    abrir()
    await pedirComEmail()
    expect(await screen.findByText('PENDENTE12345678')).toBeInTheDocument()
    expect(f.mock.calls.map(([u]) => u)).toContain('/api/pedidos/p-9/retomar')
  })

  it('retomada impossível (404) leva de volta ao mapa', async () => {
    await visitante({
      '/api/pedidos': { status: 409, corpo: { erro: 'pedido_pendente', pedido_id: 'p-9' } },
      '/api/pedidos/p-9/retomar': { status: 404, corpo: { erro: 'nao_encontrado' } },
    })
    abrir()
    await pedirComEmail()
    expect(await screen.findByRole('alert')).toHaveTextContent('O pedido anterior não pode mais ser pago.')
    expect(screen.getByRole('link', { name: 'Escolher assentos' })).toHaveAttribute('href', '/sessoes/7')
  })

  it.each([
    [409, 'holds_invalidos', 'Seus assentos expiraram ou foram liberados.'],
    [400, 'dados_invalidos', 'Informe um e-mail válido'],
    [503, 'pagamento_indisponivel', 'O pagamento está indisponível agora.'],
    [429, 'muitas_requisicoes', 'Muitas tentativas seguidas.'],
    [500, 'erro_interno', 'Não conseguimos abrir o pedido agora.'],
  ])('%i %s → mensagem própria', async (status, erro, texto) => {
    await visitante({ '/api/pedidos': { status, corpo: { erro } } })
    abrir()
    await pedirComEmail()
    expect(await screen.findByRole('alert')).toHaveTextContent(texto)
  })

  it('sem assento reservado nesta sessão', async () => {
    await visitante({ '/api/holds': { corpo: [holds[2]] } })
    abrir()
    expect(await screen.findByRole('heading', { name: 'Nenhum assento reservado' })).toBeInTheDocument()
  })
})
