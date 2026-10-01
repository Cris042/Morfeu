import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { iniciarSessao } from '../../api/sessao'
import { App } from '../../app/App'
import { paraUTCDoCinema } from '../../ui/formato'
import { apiFalsa } from '../../test/renderizar'

// Sessões e pedidos do operador (PRD 0039).

const sessaoOk = { corpo: { access_token: 'tok', expira_em: 600 } }
const op = { id: 'op1', nome: 'Olga', email: 'olga@morfeu.dev', papel: 'operador' }
const filmes = { corpo: [{ id: 1, titulo: 'Duna' }, { id: 2, titulo: 'Velho', arquivado_em: '2099-01-01T00:00:00Z' }] }
const salas = { corpo: [{ id: 3, nome: 'Sala 1', layout: { fileiras: 5, colunas: 8, vaos: [], pcd: [] } }] }
const sessao = { id: 7, filme_id: 1, sala_id: 3, inicio: '2099-10-01T23:30:00Z', fim: '2099-10-02T02:00:00Z', duracao_min: 130, preco_centavos: 3200, status: 'agendada' }
const pedidoId = '6f1c2e0a-0000-4000-8000-000000000001'
const pedidoOp = { id: pedidoId, sessao_id: 7, email: 'a***@exemplo.com', assentos: ['C4'], total_centavos: 3200, status: 'pago', criado_em: '2099-09-30T12:00:00Z' }

afterEach(() => {
  vi.unstubAllGlobals()
})

async function comoOperador(rotas: Parameters<typeof apiFalsa>[0]) {
  const f = apiFalsa({ '/api/auth/refresh': sessaoOk, '/api/auth/eu': { corpo: op }, '/api/backoffice/filmes': filmes, '/api/backoffice/salas': salas, ...rotas })
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

describe('horário do cinema', () => {
  it('datetime-local no fuso do cinema vira UTC', () => {
    expect(paraUTCDoCinema('2099-10-01T20:30')).toBe('2099-10-01T23:30:00.000Z')
  })
})

describe('sessões', () => {
  it('programa com filme ativo, sala, início no fuso do cinema e preço em centavos', async () => {
    const f = await comoOperador({
      '/api/backoffice/sessoes': { status: 201, corpo: sessao },
    })
    abrir('/backoffice/sessoes')
    const filme = await screen.findByLabelText('Filme')
    await screen.findByRole('option', { name: 'Duna' })
    expect(within(filme).queryByRole('option', { name: 'Velho' })).not.toBeInTheDocument()
    await userEvent.selectOptions(filme, 'Duna')
    await screen.findByRole('option', { name: 'Sala 1' })
    await userEvent.selectOptions(screen.getByLabelText('Sala'), 'Sala 1')
    await userEvent.type(screen.getByLabelText('Início (horário do cinema)'), '2099-10-01T20:30')
    await userEvent.type(screen.getByLabelText('Preço por assento (R$)'), '32,50')
    await userEvent.click(screen.getByRole('button', { name: 'Programar sessão' }))
    expect(await screen.findByText('Sessão programada.')).toBeInTheDocument()
    const criar = f.mock.calls.find(([u, init]) => u === '/api/backoffice/sessoes' && init?.method === 'POST')
    expect(criar?.[1]?.body).toBe('{"filme_id":1,"sala_id":3,"inicio":"2099-10-01T23:30:00.000Z","preco_centavos":3250}')
  })

  it('conflito de horário explicado', async () => {
    await comoOperador({ '/api/backoffice/sessoes': { status: 409, corpo: { erro: 'conflito_horario' } } })
    abrir('/backoffice/sessoes')
    await screen.findByRole('option', { name: 'Duna' })
    await userEvent.selectOptions(screen.getByLabelText('Filme'), 'Duna')
    await screen.findByRole('option', { name: 'Sala 1' })
    await userEvent.selectOptions(screen.getByLabelText('Sala'), 'Sala 1')
    await userEvent.type(screen.getByLabelText('Início (horário do cinema)'), '2099-10-01T20:30')
    await userEvent.type(screen.getByLabelText('Preço por assento (R$)'), '32')
    await userEvent.click(screen.getByRole('button', { name: 'Programar sessão' }))
    expect(await screen.findByText(/Conflito de horário/)).toBeInTheDocument()
  })

  it('cancelar mostra quantos pedidos pagos serão estornados antes de confirmar', async () => {
    const f = await comoOperador({
      '/api/backoffice/sessoes': { corpo: [sessao] },
      '/api/backoffice/pedidos?sessao_id=7&status=pago&pagina=1': { corpo: { pedidos: [pedidoOp, { ...pedidoOp, id: 'p2' }] } },
      '/api/backoffice/sessoes/7/cancelar': { corpo: { pedidos_estornados: 2 } },
    })
    abrir('/backoffice/sessoes')
    expect(await screen.findByText(/Sala 1 · .* · R\$ 32,00/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /^Cancelar sessão Duna — Sala 1/ }))
    expect(await screen.findByText('2 pedido(s) pago(s) serão estornados.')).toBeInTheDocument()
    expect(f.mock.calls.some(([u]) => u === '/api/backoffice/sessoes/7/cancelar')).toBe(false)
    await userEvent.click(screen.getByRole('button', { name: 'Confirmar cancelamento' }))
    expect(await screen.findByText('Sessão cancelada; 2 pedido(s) em estorno.')).toBeInTheDocument()
  })
})

describe('pedidos', () => {
  it('filtra por sessão e situação; e-mail mascarado; abre o detalhe', async () => {
    const f = await comoOperador({
      '/api/backoffice/pedidos?pagina=1': { corpo: { pedidos: [pedidoOp] } },
      '/api/backoffice/pedidos?sessao_id=7&status=pago&pagina=1': { corpo: { pedidos: [pedidoOp] } },
      [`/api/backoffice/pedidos/${pedidoId}`]: {
        corpo: {
          pedido: pedidoOp,
          ingressos: [{ assento: 'C4', status: 'ativo' }],
          eventos: [{ de: '', para: 'aguardando_pagamento', ocorrido_em: '2099-09-30T12:00:00Z' }, { de: 'aguardando_pagamento', para: 'pago', ocorrido_em: '2099-09-30T12:01:00Z' }],
        },
      },
    })
    abrir('/backoffice/pedidos')
    await userEvent.type(await screen.findByLabelText('Sessão (nº)'), '7')
    await userEvent.selectOptions(screen.getByLabelText('Situação'), 'Pago')
    await userEvent.click(screen.getByRole('button', { name: 'Filtrar' }))
    await userEvent.click(await screen.findByRole('link', { name: 'a***@exemplo.com — sessão 7' }))
    expect(f.mock.calls.some(([u]) => u === '/api/backoffice/pedidos?sessao_id=7&status=pago&pagina=1')).toBe(true)
    expect(await screen.findByRole('heading', { name: 'Pedido de a***@exemplo.com' })).toBeInTheDocument()
    expect(screen.getByText('C4 (ativo)')).toBeInTheDocument()
    expect(screen.getByText(/Aguardando pagamento → Pago/)).toBeInTheDocument()
  })

  it('cancela pelo operador com confirmação; sessão iniciada explicada', async () => {
    await comoOperador({
      [`/api/backoffice/pedidos/${pedidoId}`]: { corpo: { pedido: pedidoOp, ingressos: [], eventos: [] } },
      [`/api/backoffice/pedidos/${pedidoId}/cancelar`]: { status: 409, corpo: { erro: 'sessao_iniciada' } },
    })
    abrir(`/backoffice/pedidos/${pedidoId}`)
    await userEvent.click(await screen.findByRole('button', { name: 'Cancelar pedido' }))
    await userEvent.click(screen.getByRole('button', { name: 'Confirmar cancelamento' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('A sessão já começou')
  })
})
