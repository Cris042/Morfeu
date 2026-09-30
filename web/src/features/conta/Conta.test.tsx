import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { iniciarSessao } from '../../api/sessao'
import type { Pedido } from '../../api/tipos'
import { App } from '../../app/App'
import { apiFalsa } from '../../test/renderizar'
import { destinoSeguro } from './Entrar'

const ana = { id: 'u1', nome: 'Ana', email: 'ana@exemplo.com', papel: 'cliente' }
const sessaoOk = { corpo: { access_token: 'tok', expira_em: 600 } }

const pedido: Pedido = {
  id: '6f1c2e0a-0000-4000-8000-000000000001',
  codigo: 'ABCD2345EFGH6789',
  sessao_id: 7,
  assentos: ['C4', 'C5'],
  total_centavos: 6400,
  status: 'pago',
  expira_em: '2099-10-01T23:40:00Z',
}

afterEach(() => {
  vi.unstubAllGlobals()
})

async function comoVisitante(rotas: Parameters<typeof apiFalsa>[0] = {}) {
  const f = apiFalsa({ '/api/auth/refresh': { status: 401, corpo: { erro: 'sessao_invalida' } }, ...rotas })
  await iniciarSessao()
  return f
}

async function comoAna(rotas: Parameters<typeof apiFalsa>[0] = {}) {
  const f = apiFalsa({ '/api/auth/refresh': sessaoOk, '/api/auth/eu': { corpo: ana }, ...rotas })
  await iniciarSessao()
  return f
}

function abrir(caminho: string) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={[caminho]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('destino após o login', () => {
  it('só aceita caminho local', () => {
    expect(destinoSeguro('/sessoes/7')).toBe('/sessoes/7')
    expect(destinoSeguro(null)).toBe('/conta/pedidos')
    expect(destinoSeguro('//evil.example')).toBe('/conta/pedidos')
    expect(destinoSeguro('/\\evil.example')).toBe('/conta/pedidos')
    expect(destinoSeguro('https://evil.example')).toBe('/conta/pedidos')
  })
})

describe('login e cadastro', () => {
  it('entra e volta para onde estava; o menu mostra a conta', async () => {
    const f = await comoVisitante({
      '/api/auth/login': sessaoOk,
      '/api/auth/eu': { corpo: ana },
      '/api/pedidos?pagina=1': { corpo: { pedidos: [] } },
    })
    abrir('/entrar?volta=%2Fconta%2Fpedidos')
    await userEvent.type(screen.getByLabelText('E-mail'), 'ana@exemplo.com')
    await userEvent.type(screen.getByLabelText('Senha'), 'segredo123')
    await userEvent.click(screen.getByRole('button', { name: 'Entrar' }))
    expect(await screen.findByRole('heading', { name: 'Meus pedidos' })).toBeInTheDocument()
    expect(await screen.findByText(/Você ainda não comprou ingressos/)).toBeInTheDocument()
    const menu = screen.getByRole('navigation', { name: 'Conta' })
    expect(menu).toHaveTextContent('Ana')
    const login = f.mock.calls.find(([u]) => u === '/api/auth/login')
    expect(login?.[1]?.body).toBe('{"email":"ana@exemplo.com","senha":"segredo123"}')
  })

  it('credenciais inválidas: mensagem única, sem dizer qual campo errou', async () => {
    await comoVisitante({ '/api/auth/login': { status: 401, corpo: { erro: 'credenciais_invalidas' } } })
    abrir('/entrar')
    await userEvent.type(screen.getByLabelText('E-mail'), 'ana@exemplo.com')
    await userEvent.type(screen.getByLabelText('Senha'), 'errada123')
    await userEvent.click(screen.getByRole('button', { name: 'Entrar' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('E-mail ou senha incorretos.')
  })

  it('cadastro recusado mostra o motivo por campo e o 429', async () => {
    await comoVisitante({ '/api/auth/registro': { status: 400, corpo: { erro: 'dados_invalidos', campos: ['senha'] } } })
    abrir('/cadastro')
    await userEvent.type(screen.getByLabelText('Nome'), 'Ana')
    await userEvent.type(screen.getByLabelText('E-mail'), 'ana@exemplo.com')
    await userEvent.type(screen.getByLabelText('Senha'), '12345678')
    await userEvent.click(screen.getByRole('button', { name: 'Criar conta' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('A senha precisa ter de 8 a 128 caracteres.')

    apiFalsa({ '/api/auth/registro': { status: 429, corpo: { erro: 'muitas_tentativas' } } })
    await userEvent.click(screen.getByRole('button', { name: 'Criar conta' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Muitas tentativas.')
  })

  it('cadastro bem-sucedido já entra na conta', async () => {
    const f = await comoVisitante({
      '/api/auth/registro': { status: 201, corpo: ana },
      '/api/auth/login': sessaoOk,
      '/api/auth/eu': { corpo: ana },
      '/api/pedidos?pagina=1': { corpo: { pedidos: [] } },
    })
    abrir('/cadastro')
    await userEvent.type(screen.getByLabelText('Nome'), 'Ana')
    await userEvent.type(screen.getByLabelText('E-mail'), 'ana@exemplo.com')
    await userEvent.type(screen.getByLabelText('Senha'), 'segredo123')
    await userEvent.click(screen.getByRole('button', { name: 'Criar conta' }))
    expect(await screen.findByRole('heading', { name: 'Meus pedidos' })).toBeInTheDocument()
    expect(f.mock.calls.map(([u]) => u)).toContain('/api/auth/login')
  })
})

describe('meus pedidos', () => {
  it('visitante é levado ao login com a volta preservada', async () => {
    await comoVisitante()
    abrir('/conta/pedidos')
    expect(await screen.findByRole('heading', { name: 'Entrar' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Criar conta' })).toHaveAttribute('href', '/cadastro?volta=%2Fconta%2Fpedidos')
  })

  it('lista com situação, assentos e total; abre o detalhe com o Bearer', async () => {
    const f = await comoAna({
      '/api/pedidos?pagina=1': { corpo: { pedidos: [pedido] } },
      [`/api/pedidos/${pedido.id}`]: { corpo: pedido },
    })
    abrir('/conta/pedidos')
    const link = await screen.findByRole('link', { name: 'Pedido ABCD2345EFGH6789' })
    expect(screen.getByText('Pago')).toBeInTheDocument()
    expect(screen.getByText('Assentos C4, C5')).toBeInTheDocument()
    expect(screen.getByText('R$ 64,00')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mais antigos' })).not.toBeInTheDocument()
    await userEvent.click(link)
    expect(await screen.findByRole('heading', { name: 'Pedido ABCD2345EFGH6789' })).toBeInTheDocument()
    expect(screen.getByText(/enviados ao e-mail/)).toBeInTheDocument()
    const detalhe = f.mock.calls.find(([u]) => u === `/api/pedidos/${pedido.id}`)
    expect((detalhe?.[1]?.headers as Record<string, string>).Authorization).toBe('Bearer tok')
  })

  it('página cheia oferece a próxima', async () => {
    const cheia = Array.from({ length: 20 }, (_, i) => ({ ...pedido, id: `p${String(i)}`, codigo: `COD${String(i)}` }))
    await comoAna({
      '/api/pedidos?pagina=1': { corpo: { pedidos: cheia } },
      '/api/pedidos?pagina=2': { corpo: { pedidos: [{ ...pedido, codigo: 'ANTIGO' }] } },
    })
    abrir('/conta/pedidos')
    await userEvent.click(await screen.findByRole('button', { name: 'Mais antigos' }))
    expect(await screen.findByRole('link', { name: 'Pedido ANTIGO' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Mais recentes' })).toBeInTheDocument()
  })

  it('pedido de outra conta (404) não vaza nada', async () => {
    await comoAna({ '/api/pedidos/alheio': { status: 404, corpo: { erro: 'nao_encontrado' } } })
    abrir('/conta/pedidos/alheio')
    expect(await screen.findByText(/Pedido não encontrado nesta conta/)).toBeInTheDocument()
  })

  it('sair esquece a conta e volta ao cartaz', async () => {
    await comoAna({
      '/api/pedidos?pagina=1': { corpo: { pedidos: [pedido] } },
      '/api/auth/refresh/logout': { status: 204 },
      '/api/filmes': { corpo: [] },
    })
    abrir('/conta/pedidos')
    await screen.findByRole('link', { name: 'Pedido ABCD2345EFGH6789' })
    await userEvent.click(screen.getByRole('button', { name: 'Sair' }))
    expect(await screen.findByRole('link', { name: 'Entrar' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Escolha o filme da noite' })).toBeInTheDocument()
  })
})
