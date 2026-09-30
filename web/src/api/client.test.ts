import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, ErroApi } from './client'

function responder(status: number, corpo?: unknown) {
  const fetchFalso = vi.fn<typeof fetch>().mockResolvedValue(
    new Response(corpo === undefined ? null : JSON.stringify(corpo), { status }),
  )
  vi.stubGlobal('fetch', fetchFalso)
  return fetchFalso
}

function chamada(fetchFalso: ReturnType<typeof responder>) {
  const [url, init] = fetchFalso.mock.calls[0] ?? []
  return { url, init: init ?? {}, headers: (init?.headers ?? {}) as Record<string, string> }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('cliente da API', () => {
  it('GET usa /api, envia o cookie e não manda o header anti-CSRF', async () => {
    const f = responder(200, [{ id: 1 }])
    await expect(api.get('/filmes')).resolves.toEqual([{ id: 1 }])
    const { url, init, headers } = chamada(f)
    expect(url).toBe('/api/filmes')
    expect(init.credentials).toBe('include')
    expect(init.referrerPolicy).toBe('no-referrer')
    expect(headers['X-Requested-With']).toBeUndefined()
  })

  it('POST envia JSON, cookie e X-Requested-With', async () => {
    const f = responder(201, { holds: [] })
    await api.post('/sessoes/7/holds', { assentos: ['F7'] })
    const { init, headers } = chamada(f)
    expect(init.method).toBe('POST')
    expect(init.credentials).toBe('include')
    expect(init.body).toBe('{"assentos":["F7"]}')
    expect(headers['X-Requested-With']).toBe('morfeu')
    expect(headers['Content-Type']).toBe('application/json')
  })

  it('DELETE manda o header anti-CSRF e 204 vira undefined', async () => {
    const f = responder(204)
    await expect(api.delete('/holds/abc')).resolves.toBeUndefined()
    expect(chamada(f).headers['X-Requested-With']).toBe('morfeu')
  })

  it('409 vira ErroApi com o código e o corpo', async () => {
    responder(409, { erro: 'assento_indisponivel', assentos: ['F8'] })
    const erro = await api.post('/sessoes/7/holds', { assentos: ['F8'] }).catch((e: unknown) => e)
    expect(erro).toBeInstanceOf(ErroApi)
    const e = erro as ErroApi
    expect(e.status).toBe(409)
    expect(e.codigo).toBe('assento_indisponivel')
    expect(e.corpo).toEqual({ erro: 'assento_indisponivel', assentos: ['F8'] })
  })

  it('falha de rede vira ErroApi com status 0', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockRejectedValue(new TypeError('offline')))
    const erro = await api.get('/filmes').catch((e: unknown) => e)
    expect(erro).toBeInstanceOf(ErroApi)
    expect((erro as ErroApi).status).toBe(0)
  })
})
