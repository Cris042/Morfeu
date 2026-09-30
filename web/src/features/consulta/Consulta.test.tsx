import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiFalsa, renderizar } from '../../test/renderizar'
import { Consulta } from './Consulta'
import { Ingresso } from './Ingresso'

afterEach(() => {
  vi.unstubAllGlobals()
})

const ref = '0b9e6c62-51a5-4bd6-9a55-2f0a8b3c4d5e.abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ'
const pedido = {
  id: 'p-1',
  codigo: 'ABCD2345EFGH6789',
  sessao_id: 7,
  assentos: ['C4', 'C5'],
  total_centavos: 6400,
  status: 'pago',
  expira_em: '2099-10-01T23:30:00Z',
}

async function consultar(email: string, codigo: string) {
  await userEvent.type(screen.getByLabelText('E-mail'), email)
  await userEvent.type(screen.getByLabelText('Código do pedido'), codigo)
  await userEvent.click(screen.getByRole('button', { name: 'Consultar' }))
}

describe('consulta de convidado', () => {
  it('acerto mostra o pedido e os links dos ingressos', async () => {
    const f = apiFalsa({
      '/api/pedidos/consulta': { corpo: { pedido, ingressos: [{ assento: 'C4', ref }, { assento: 'C5', ref: ref.replace('0b9e', '1b9e') }] } },
    })
    renderizar(<Consulta />)
    await consultar('ana@exemplo.com', 'abcd-2345-efgh-6789')
    expect(await screen.findByRole('heading', { name: 'Pedido ABCD2345EFGH6789' })).toBeInTheDocument()
    expect(screen.getByText('Pago')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Ingresso C4' })).toHaveAttribute('href', `/i/${ref}`)
    const init = f.mock.calls[0]?.[1]
    expect(init?.body).toBe('{"email":"ana@exemplo.com","codigo":"abcd-2345-efgh-6789"}')
    expect((init?.headers as Record<string, string>)['X-Requested-With']).toBe('morfeu')
    expect(window.localStorage.length + window.sessionStorage.length).toBe(0)
  })

  it('pedido ainda não pago: sem links, com aviso', async () => {
    apiFalsa({ '/api/pedidos/consulta': { corpo: { pedido: { ...pedido, status: 'aguardando_pagamento' }, ingressos: [] } } })
    renderizar(<Consulta />)
    await consultar('ana@exemplo.com', 'ABCD2345EFGH6789')
    expect(await screen.findByText(/aparecem aqui quando o pagamento é confirmado/)).toBeInTheDocument()
  })

  it.each([
    [404, 'Não encontramos um pedido com esse e-mail e código.'],
    [429, 'Muitas consultas seguidas.'],
    [500, 'Não conseguimos consultar agora.'],
  ])('%i → mensagem única', async (status, texto) => {
    apiFalsa({ '/api/pedidos/consulta': { status, corpo: { erro: 'x' } } })
    renderizar(<Consulta />)
    await consultar('ana@exemplo.com', 'ABCD2345EFGH6789')
    expect(await screen.findByRole('alert')).toHaveTextContent(texto)
  })
})

describe('página do ingresso', () => {
  it('válido: filme, sessão no fuso do cinema, sala, assento e QR sem referrer', async () => {
    const f = apiFalsa({
      [`/api/i/${ref}`]: { corpo: { assento: 'C4', status: 'ativo', filme: 'Duna', sala: 'Sala 1', inicio: '2099-10-01T23:30:00Z' } },
    })
    renderizar(<Ingresso />, { caminho: `/i/${ref}`, padrao: '/i/:ref' })
    expect(await screen.findByRole('heading', { name: 'Duna' })).toBeInTheDocument()
    expect(screen.getByText(/20:30/)).toBeInTheDocument()
    expect(screen.getByText('Sala 1')).toBeInTheDocument()
    const qr = screen.getByRole('img', { name: 'QR do ingresso do assento C4' })
    expect(qr).toHaveAttribute('src', `/api/i/${ref}/qr.png`)
    expect(qr).toHaveAttribute('referrerpolicy', 'no-referrer')
    expect(f.mock.calls[0]?.[1]?.referrerPolicy).toBe('no-referrer')
  })

  it('usado: avisa', async () => {
    apiFalsa({ [`/api/i/${ref}`]: { corpo: { assento: 'C4', status: 'usado', filme: 'Duna', sala: 'Sala 1', inicio: '2099-10-01T23:30:00Z' } } })
    renderizar(<Ingresso />, { caminho: `/i/${ref}`, padrao: '/i/:ref' })
    expect(await screen.findByText('Este ingresso já foi usado na entrada.')).toBeInTheDocument()
  })

  it.each([
    [404, 'Link de ingresso inválido'],
    [410, 'Este ingresso não vale mais'],
  ])('%i → %s, sem QR', async (status, titulo) => {
    apiFalsa({ [`/api/i/${ref}`]: { status, corpo: { erro: 'x' } } })
    renderizar(<Ingresso />, { caminho: `/i/${ref}`, padrao: '/i/:ref' })
    expect(await screen.findByRole('heading', { name: titulo })).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Consultar pedido' })).toHaveAttribute('href', '/consulta')
  })
})
