import { focusManager } from '@tanstack/react-query'
import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Hold } from '../../api/tipos'
import { renderizar } from '../../test/renderizar'
import { mapaFake } from '../mapa/fixture'
import { PaginaSessao } from './PaginaSessao'

// Relógio fixo: holds expiram relativos a ele (sem sleep — ADR 0006).
const T0 = new Date('2099-10-01T20:00:00Z')

type Resposta = { status?: number; corpo?: unknown }
type Rota = Resposta | ((corpo: unknown) => Resposta)

/** API falsa por "MÉTODO caminho"; conta chamadas e guarda os corpos. */
function api(rotas: Record<string, Rota>) {
  const chamadas: { chave: string; corpo: unknown }[] = []
  const fetchFalso = vi.fn<typeof fetch>((entrada, init) => {
    const url = typeof entrada === 'string' ? entrada : entrada instanceof URL ? entrada.pathname : entrada.url
    const chave = `${init?.method ?? 'GET'} ${url}`
    const corpo: unknown = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined
    chamadas.push({ chave, corpo })
    const rota = rotas[chave]
    const r = typeof rota === 'function' ? rota(corpo) : (rota ?? { status: 500, corpo: { erro: 'erro_interno' } })
    return Promise.resolve(
      new Response(r.corpo === undefined ? null : JSON.stringify(r.corpo), { status: r.status ?? 200 }),
    )
  })
  vi.stubGlobal('fetch', fetchFalso)
  const quantas = (chave: string) => chamadas.filter((c) => c.chave === chave).length
  return { chamadas, quantas }
}

function hold(assento: string, minutos: number, extensoes = 0): Hold {
  return {
    id: `h-${assento}`,
    sessao_id: 1,
    assento,
    expira_em: new Date(T0.getTime() + minutos * 60_000).toISOString(),
    extensoes_usadas: extensoes,
  }
}

const base = {
  'GET /api/sessoes/1/mapa': { corpo: mapaFake },
  'GET /api/sessoes/1/ocupacao': { corpo: { sessao_id: 1, ocupados: ['C6'] } },
  'GET /api/holds': { corpo: [] },
}

function abrir() {
  renderizar(<PaginaSessao />, { caminho: '/sessoes/1', padrao: '/sessoes/:id' })
}

const botao = (nome: RegExp) => screen.getByRole('button', { name: nome })

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  vi.setSystemTime(T0)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  focusManager.setFocused(undefined)
})

describe('página da sessão', () => {
  it('polling de 4 s na ocupação; mapa buscado uma vez; pausa sem foco', async () => {
    const { quantas } = api(base)
    abrir()
    expect(await screen.findByRole('grid', { name: 'Assentos da Sala 1' })).toBeInTheDocument()
    // Cada fileira tem um rowheader (ARIA grid: row só contém células — auditoria 0019).
    expect(screen.getAllByRole('rowheader')).toHaveLength(6)
    expect(screen.getByRole('rowheader', { name: 'Fileira A' })).toBeInTheDocument()
    await waitFor(() => {
      expect(quantas('GET /api/sessoes/1/ocupacao')).toBe(1)
    })

    await act(() => vi.advanceTimersByTimeAsync(4000))
    expect(quantas('GET /api/sessoes/1/ocupacao')).toBe(2)
    await act(() => vi.advanceTimersByTimeAsync(4000))
    expect(quantas('GET /api/sessoes/1/ocupacao')).toBe(3)
    expect(quantas('GET /api/sessoes/1/mapa')).toBe(1)

    act(() => {
      focusManager.setFocused(false)
    })
    await act(() => vi.advanceTimersByTimeAsync(12_000))
    expect(quantas('GET /api/sessoes/1/ocupacao')).toBe(3)
  })

  it('reservar só mostra "seu" depois da resposta do servidor', async () => {
    let holds: Hold[] = []
    const { chamadas } = api({
      ...base,
      'GET /api/holds': () => ({ corpo: holds }),
      'POST /api/sessoes/1/holds': (corpo) => {
        holds = (corpo as { assentos: string[] }).assentos.map((a) => hold(a, 10))
        return { status: 201, corpo: { holds } }
      },
    })
    abrir()
    const u = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) })
    await u.click(await screen.findByRole('button', { name: /^Fileira B, assento 2, livre$/ }))
    await u.click(botao(/^Fileira B, assento 3, livre$/))
    expect(botao(/^Reservar 2 assentos$/)).toBeEnabled()
    expect(screen.queryByRole('button', { name: /reservado para você/ })).toBeNull()

    await u.click(botao(/^Reservar 2 assentos$/))
    expect(chamadas.find((c) => c.chave === 'POST /api/sessoes/1/holds')?.corpo).toEqual({ assentos: ['B2', 'B3'] })
    expect(await screen.findByRole('button', { name: /^Fileira B, assento 2, reservado para você$/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Seus assentos' })).toBeInTheDocument()
    expect(botao(/^Reservar assento$/)).toBeDisabled()
  })

  it('409: ocupação recarregada na hora, aviso com o assento perdido, que sai da seleção', async () => {
    let ocupados = ['C6']
    const { quantas } = api({
      ...base,
      'GET /api/sessoes/1/ocupacao': () => ({ corpo: { sessao_id: 1, ocupados } }),
      'POST /api/sessoes/1/holds': () => {
        ocupados = ['C6', 'B3']
        return { status: 409, corpo: { erro: 'assento_indisponivel', assentos: ['B3'] } }
      },
    })
    abrir()
    const u = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) })
    await u.click(await screen.findByRole('button', { name: /^Fileira B, assento 2, livre$/ }))
    await u.click(botao(/^Fileira B, assento 3, livre$/))
    const antes = quantas('GET /api/sessoes/1/ocupacao')
    await u.click(botao(/^Reservar 2 assentos$/))

    expect(await screen.findByText('O assento B3 acabou de ser reservado por outra pessoa. Escolha outro.')).toBeInTheDocument()
    await waitFor(() => {
      expect(quantas('GET /api/sessoes/1/ocupacao')).toBe(antes + 1) // sem esperar o polling
    })
    expect(await screen.findByRole('button', { name: /^Fileira B, assento 3, ocupado$/ })).toBeInTheDocument()
    expect(botao(/^Fileira B, assento 2, selecionado$/)).toBeInTheDocument()
    expect(botao(/^Reservar assento$/)).toBeEnabled()
  })

  it('limite de holds e 429 têm mensagens próprias', async () => {
    let status = 409
    api({
      ...base,
      'POST /api/sessoes/1/holds': () =>
        status === 409 ? { status, corpo: { erro: 'limite_holds' } } : { status, corpo: { erro: 'muitas_requisicoes' } },
    })
    abrir()
    const u = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) })
    await u.click(await screen.findByRole('button', { name: /^Fileira A, assento 3, livre$/ }))
    await u.click(botao(/^Reservar assento$/))
    expect(await screen.findByText('Você já tem 6 assentos reservados — o máximo por compra.')).toBeInTheDocument()
    status = 429
    await u.click(botao(/^Reservar assento$/))
    expect(await screen.findByText('Muitas tentativas seguidas. Espere um minuto e tente de novo.')).toBeInTheDocument()
  })

  it('selecionado tomado por outra pessoa no polling sai da seleção com aviso', async () => {
    let ocupados = ['C6']
    api({ ...base, 'GET /api/sessoes/1/ocupacao': () => ({ corpo: { sessao_id: 1, ocupados } }) })
    abrir()
    const u = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) })
    await u.click(await screen.findByRole('button', { name: /^Fileira D, assento 7, livre$/ }))
    ocupados = ['C6', 'D7']
    await act(() => vi.advanceTimersByTimeAsync(4000))
    expect(await screen.findByText('D7 acabou de ser reservado por outra pessoa.')).toBeInTheDocument()
    expect(botao(/^Fileira D, assento 7, ocupado$/)).toBeInTheDocument()
    expect(botao(/^Reservar assento$/)).toBeDisabled()
  })

  it('painel: contagem, aviso de 1 minuto, extensão única e liberação', async () => {
    let holds: Hold[] = [hold('A6', 10)]
    const { quantas } = api({
      ...base,
      'GET /api/sessoes/1/ocupacao': { corpo: { sessao_id: 1, ocupados: ['C6', 'A6'] } },
      'GET /api/holds': () => ({ corpo: holds }),
      'POST /api/holds/h-A6/estender': () => {
        holds = [hold('A6', 20, 1)]
        return { corpo: holds[0] }
      },
      'DELETE /api/holds/h-A6': () => {
        holds = []
        return { status: 204 }
      },
    })
    abrir()
    expect(await screen.findByText('10:00')).toBeInTheDocument()
    expect(botao(/^Fileira A, assento 6, reservado para você$/)).toBeInTheDocument()
    await act(() => vi.advanceTimersByTimeAsync(1000))
    expect(screen.getByText('09:59')).toBeInTheDocument()

    await act(() => vi.advanceTimersByTimeAsync(9 * 60_000))
    expect(screen.getByText('Seu assento expira em menos de um minuto.')).toBeInTheDocument()

    const u = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) })
    await u.click(botao(/^Mais 10 minutos$/))
    await waitFor(() => {
      expect(botao(/^Mais 10 minutos$/)).toBeDisabled()
    })
    expect(quantas('POST /api/holds/h-A6/estender')).toBe(1)
    expect(screen.queryByText('Seu assento expira em menos de um minuto.')).toBeNull()

    await u.click(botao(/^Liberar A6$/))
    await waitFor(() => {
      expect(screen.queryByRole('heading', { name: 'Seus assentos' })).toBeNull()
    })
    expect(quantas('DELETE /api/holds/h-A6')).toBe(1)
  })

  it('hold vencido reconsulta os holds e some do painel', async () => {
    let holds: Hold[] = [hold('A7', 1)]
    const { quantas } = api({ ...base, 'GET /api/holds': () => ({ corpo: holds }) })
    abrir()
    expect(await screen.findByText('01:00')).toBeInTheDocument()
    holds = []
    const antes = quantas('GET /api/holds')
    await act(() => vi.advanceTimersByTimeAsync(61_000))
    await waitFor(() => {
      expect(quantas('GET /api/holds')).toBeGreaterThan(antes)
    })
    expect(screen.queryByRole('heading', { name: 'Seus assentos' })).toBeNull()
  })
})
