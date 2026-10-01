import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { iniciarSessao } from '../../api/sessao'
import { App } from '../../app/App'
import { apiFalsa } from '../../test/renderizar'

// Backoffice (PRD 0038): guarda de papel (só experiência — a API decide),
// filmes e salas.

const sessaoOk = { corpo: { access_token: 'tok', expira_em: 600 } }
const op = { id: 'op1', nome: 'Olga', email: 'olga@morfeu.dev', papel: 'operador' }
const ana = { id: 'u1', nome: 'Ana', email: 'ana@exemplo.com', papel: 'cliente' }
const salaUm = { id: 3, nome: 'Sala 1', layout: { fileiras: 5, colunas: 8, vaos: [], pcd: [] } }

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function como(usuario: typeof op | undefined, rotas: Parameters<typeof apiFalsa>[0] = {}) {
  const f = apiFalsa(
    usuario
      ? { '/api/auth/refresh': sessaoOk, '/api/auth/eu': { corpo: usuario }, ...rotas }
      : { '/api/auth/refresh': { status: 401, corpo: { erro: 'sessao_invalida' } }, ...rotas },
  )
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

describe('guarda de papel', () => {
  it('visitante vai ao login com a volta', async () => {
    await como(undefined)
    abrir('/backoffice/filmes')
    expect(await screen.findByRole('heading', { name: 'Entrar' })).toBeInTheDocument()
  })

  it('cliente vê "área restrita" e nenhuma chamada ao backoffice', async () => {
    const f = await como(ana)
    abrir('/backoffice/filmes')
    expect(await screen.findByRole('heading', { name: 'Área restrita' })).toBeInTheDocument()
    expect(f.mock.calls.some(([u]) => typeof u === 'string' && u.startsWith('/api/backoffice'))).toBe(false)
    expect(screen.queryByRole('link', { name: 'Backoffice' })).not.toBeInTheDocument()
  })

  it('operador entra pelo menu', async () => {
    await como(op, { '/api/backoffice/filmes': { corpo: [] } })
    abrir('/conta/pedidos')
    await userEvent.click(await screen.findByRole('link', { name: 'Backoffice' }))
    expect(await screen.findByText(/Nenhum filme ainda/)).toBeInTheDocument()
  })
})

describe('filmes', () => {
  it('lista, busca no TMDB e importa; arquivar pede confirmação', async () => {
    const f = await como(op, {
      '/api/backoffice/filmes': { corpo: [{ id: 1, titulo: 'Duna', ano: 2021, duracao_min: 155 }, { id: 2, titulo: 'Antigo', arquivado_em: '2099-01-01T00:00:00Z' }] },
      '/api/backoffice/filmes/tmdb?q=matrix': { corpo: { resultados: [{ tmdb_id: 603, titulo: '<b>Matrix</b>', ano: 1999 }] } },
      '/api/backoffice/filmes/importar': { status: 201, corpo: { filme: { id: 9, titulo: 'Matrix' }, criado: true } },
      '/api/backoffice/filmes/1/arquivar': { status: 204 },
    })
    abrir('/backoffice/filmes')
    expect(await screen.findByText('Duna')).toBeInTheDocument()
    expect(screen.getByText('2021 · 155 min')).toBeInTheDocument()
    expect(screen.getByText('Arquivado')).toBeInTheDocument()

    await userEvent.type(screen.getByLabelText('Título'), 'matrix')
    await userEvent.click(screen.getByRole('button', { name: 'Buscar' }))
    // Dado externo é texto: a tag não vira HTML.
    expect(await screen.findByText('<b>Matrix</b>')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Importar' }))
    expect(await screen.findByRole('status')).toHaveTextContent('entrou no catálogo')
    const importar = f.mock.calls.find(([u]) => u === '/api/backoffice/filmes/importar')
    expect(importar?.[1]?.body).toBe('{"tmdb_id":603}')

    const confirmar = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    const linha = screen.getByText('Duna').closest('li') as HTMLElement
    await userEvent.click(within(linha).getByRole('button', { name: 'Arquivar' }))
    expect(f.mock.calls.some(([u]) => u === '/api/backoffice/filmes/1/arquivar')).toBe(false)
    await userEvent.click(within(linha).getByRole('button', { name: 'Arquivar' }))
    expect(confirmar).toHaveBeenCalledTimes(2)
    expect(f.mock.calls.some(([u]) => u === '/api/backoffice/filmes/1/arquivar')).toBe(true)
  })

  it('TMDB fora do ar: mensagem própria', async () => {
    await como(op, {
      '/api/backoffice/filmes': { corpo: [] },
      '/api/backoffice/filmes/tmdb?q=duna': { status: 503, corpo: { erro: 'tmdb_indisponivel' } },
    })
    abrir('/backoffice/filmes')
    await userEvent.type(await screen.findByLabelText('Título'), 'duna')
    await userEvent.click(screen.getByRole('button', { name: 'Buscar' }))
    expect(await screen.findByText('O TMDB não respondeu. Tente de novo em instantes.')).toBeInTheDocument()
  })
})

describe('salas', () => {
  it('cria com o layout em JSON e recusa JSON inválido antes da API', async () => {
    const f = await como(op, {
      '/api/backoffice/salas': { status: 201, corpo: { ...salaUm, id: 4, nome: 'Sala 2' } },
    })
    abrir('/backoffice/salas')
    const layout = await screen.findByLabelText('Layout (JSON)')
    await userEvent.type(screen.getByLabelText('Nome'), 'Sala 2')
    await userEvent.clear(layout)
    await userEvent.type(layout, 'nao é json')
    await userEvent.click(screen.getByRole('button', { name: 'Criar sala' }))
    expect(await screen.findByText('O layout não é um JSON válido.')).toBeInTheDocument()
    expect(f.mock.calls.some(([u, init]) => u === '/api/backoffice/salas' && init?.method === 'POST')).toBe(false)

    await userEvent.clear(layout)
    await userEvent.type(layout, '{{"fileiras":5,"colunas":8}')
    await userEvent.click(screen.getByRole('button', { name: 'Criar sala' }))
    expect(await screen.findByText('Sala "Sala 2" criada.')).toBeInTheDocument()
    const criar = f.mock.calls.find(([u, init]) => u === '/api/backoffice/salas' && init?.method === 'POST')
    expect(criar?.[1]?.body).toBe('{"nome":"Sala 2","layout":{"fileiras":5,"colunas":8}}')
  })

  it('editar sala com sessões: layout_em_uso explicado', async () => {
    await como(op, {
      '/api/backoffice/salas': { corpo: [salaUm] },
      '/api/backoffice/salas/3': { status: 409, corpo: { erro: 'layout_em_uso' } },
    })
    abrir('/backoffice/salas')
    await userEvent.click(await screen.findByRole('button', { name: 'Editar Sala 1' }))
    expect(screen.getByRole('heading', { name: 'Editar Sala 1' })).toBeInTheDocument()
    expect(screen.getByText('5 fileiras × 8 colunas')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Salvar sala' }))
    expect(await screen.findByText(/o layout não pode mudar/)).toBeInTheDocument()
  })
})
