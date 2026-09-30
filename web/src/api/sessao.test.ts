import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Credencial } from './sessao'
import { criarRefreshUnico } from './sessao'

const ana = { id: 'u1', nome: 'Ana', email: 'ana@exemplo.com', papel: 'cliente' as const }

function credencial(token: string): Credencial {
  return { token, expiraEm: Date.now() + 600_000, usuario: ana }
}

// Promessa controlada pelo teste.
function adiada<T>() {
  let resolver!: (v: T) => void
  let rejeitar!: (e: unknown) => void
  const promessa = new Promise<T>((res, rej) => {
    resolver = res
    rejeitar = rej
  })
  return { promessa, resolver, rejeitar }
}

describe('refresh único (puro)', () => {
  it('N chamadas simultâneas na aba → 1 refresh no servidor', async () => {
    const pendente = adiada<Credencial | null>()
    const renovar = vi.fn(() => pendente.promessa)
    const refresh = criarRefreshUnico({ renovar, geracao: () => 0, atual: () => undefined })
    const chamadas = Array.from({ length: 5 }, () => refresh())
    pendente.resolver(credencial('t1'))
    const resultados = await Promise.all(chamadas)
    expect(renovar).toHaveBeenCalledTimes(1)
    expect(new Set(resultados.map((c) => c?.token))).toEqual(new Set(['t1']))
  })

  it('falha chega a todos os que esperavam e a próxima chamada tenta de novo', async () => {
    const renovar = vi.fn<() => Promise<Credencial | null>>()
    renovar.mockRejectedValueOnce(new Error('rede')).mockResolvedValueOnce(credencial('t2'))
    const refresh = criarRefreshUnico({ renovar, geracao: () => 0, atual: () => undefined })
    const [a, b] = await Promise.allSettled([refresh(), refresh()])
    expect(a.status).toBe('rejected')
    expect(b.status).toBe('rejected')
    await expect(refresh()).resolves.toMatchObject({ token: 't2' })
    expect(renovar).toHaveBeenCalledTimes(2)
  })

  it('duas abas com lock: a segunda reaproveita o resultado e o cookie nunca é reusado', async () => {
    // Servidor com rotação e detecção de reuso: cada cookie vale uma vez.
    let cookie = 'c0'
    const usados = new Set<string>()
    let revogada = false
    let chamadasServidor = 0
    const servidor = async (): Promise<Credencial | null> => {
      chamadasServidor++
      const enviado = cookie // o navegador manda o cookie vigente na hora
      await Promise.resolve()
      if (usados.has(enviado)) {
        revogada = true
        return null
      }
      usados.add(enviado)
      cookie = `c${String(usados.size)}`
      return credencial(`t-${cookie}`)
    }

    // Lock compartilhado (fila) + canal que entrega à outra aba antes de soltar.
    let fila = Promise.resolve<unknown>(undefined)
    const travar = (fn: () => Promise<Credencial | null>) => {
      const vez = fila.then(fn)
      fila = vez.catch(() => undefined)
      return vez
    }
    const abas = [0, 1].map(() => ({ geracao: 0, atual: undefined as Credencial | undefined }))
    const refreshDe = (i: number) =>
      criarRefreshUnico({
        renovar: async () => {
          const c = await servidor()
          const outra = abas[1 - i]
          if (c && outra) {
            outra.geracao++
            outra.atual = c
          }
          return c
        },
        travar,
        geracao: () => abas[i]?.geracao ?? 0,
        atual: () => abas[i]?.atual,
      })

    const [r0, r1] = await Promise.all([refreshDe(0)(), refreshDe(1)()])
    expect(revogada).toBe(false)
    expect(chamadasServidor).toBe(1)
    expect(r0?.token).toBe('t-c1')
    expect(r1?.token).toBe('t-c1')
  })

  it('sem lock entre abas, duas renovações simultâneas derrubam a família (falha segura documentada)', async () => {
    const usados = new Set<string>()
    const servidor = () => {
      const enviado = 'c0'
      if (usados.has(enviado)) {
        return Promise.resolve(null)
      }
      usados.add(enviado)
      return Promise.resolve(credencial('t1'))
    }
    const a = criarRefreshUnico({ renovar: servidor, geracao: () => 0, atual: () => undefined })
    const b = criarRefreshUnico({ renovar: servidor, geracao: () => 0, atual: () => undefined })
    const [ra, rb] = await Promise.all([a(), b()])
    expect([ra?.token, rb]).toEqual(['t1', null])
  })
})

// ---------------------------------------------------------------------------
// Módulo real (estado da aba + cliente HTTP) contra um fetch falso.

type Rota = (init: RequestInit) => { status: number; corpo?: unknown }

function servidorFalso(rotas: Record<string, Rota>) {
  const f = vi.fn<typeof fetch>((entrada, init) => {
    const url = typeof entrada === 'string' ? entrada : entrada instanceof URL ? entrada.pathname : entrada.url
    const rota = rotas[`${init?.method ?? 'GET'} ${url}`]
    const r = rota ? rota(init ?? {}) : { status: 500, corpo: { erro: 'erro_interno' } }
    return Promise.resolve(new Response(r.corpo === undefined ? null : JSON.stringify(r.corpo), { status: r.status }))
  })
  vi.stubGlobal('fetch', f)
  return f
}

function bearer(init: RequestInit): string | undefined {
  return (init.headers as Record<string, string> | undefined)?.Authorization
}

const eu: Rota = (init) => (bearer(init) ? { status: 200, corpo: ana } : { status: 401 })

async function moduloNovo() {
  vi.resetModules()
  const sessao = await import('./sessao')
  const { api } = await import('./client')
  return { ...sessao, api }
}

describe('sessão da aba', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('login guarda o token só em memória e o cliente passa a mandar o Bearer', async () => {
    const f = servidorFalso({
      'POST /api/auth/login': () => ({ status: 200, corpo: { access_token: 'tok-login', expira_em: 600 } }),
      'GET /api/auth/eu': eu,
      'GET /api/pedidos': (init) => ({ status: 200, corpo: { pedidos: [], auth: bearer(init) } }),
    })
    const m = await moduloNovo()
    await m.entrar('ana@exemplo.com', 'segredo123')
    expect(m.estadoSessao()).toEqual({ status: 'logado', usuario: ana })
    await expect(m.api.get('/pedidos')).resolves.toEqual({ pedidos: [], auth: 'Bearer tok-login' })
    // Nada no navegador fora da memória do módulo.
    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)
    expect(document.cookie).not.toContain('tok-login')
    // Rotas /auth/* nunca levam o Bearer automático.
    const login = f.mock.calls.find(([u]) => u === '/api/auth/login')
    expect(bearer(login?.[1] ?? {})).toBeUndefined()
  })

  it('401 com token → 1 refresh e 1 nova tentativa com o token novo', async () => {
    let refreshes = 0
    const f = servidorFalso({
      'POST /api/auth/login': () => ({ status: 200, corpo: { access_token: 'velho', expira_em: 600 } }),
      'POST /api/auth/refresh': () => {
        refreshes++
        return { status: 200, corpo: { access_token: 'novo', expira_em: 600 } }
      },
      'GET /api/auth/eu': eu,
      'GET /api/pedidos': (init) =>
        bearer(init) === 'Bearer novo' ? { status: 200, corpo: { pedidos: [] } } : { status: 401 },
    })
    const m = await moduloNovo()
    await m.entrar('ana@exemplo.com', 'segredo123')
    await Promise.all([m.api.get('/pedidos'), m.api.get('/pedidos'), m.api.get('/pedidos')])
    expect(refreshes).toBe(1)
    const refresh = f.mock.calls.find(([u]) => u === '/api/auth/refresh')
    expect((refresh?.[1]?.headers as Record<string, string>)['X-Requested-With']).toBe('morfeu')
  })

  it('refresh recusado (401) → deslogado, erro original e sem laço', async () => {
    let refreshes = 0
    servidorFalso({
      'POST /api/auth/login': () => ({ status: 200, corpo: { access_token: 'velho', expira_em: 600 } }),
      'POST /api/auth/refresh': () => {
        refreshes++
        return { status: 401, corpo: { erro: 'sessao_invalida' } }
      },
      'GET /api/auth/eu': eu,
      'GET /api/pedidos': () => ({ status: 401 }),
    })
    const m = await moduloNovo()
    await m.entrar('ana@exemplo.com', 'segredo123')
    await expect(m.api.get('/pedidos')).rejects.toMatchObject({ status: 401 })
    expect(refreshes).toBe(1)
    expect(m.estadoSessao()).toEqual({ status: 'anonimo' })
    // Deslogado: próxima chamada vai sem Bearer e não tenta renovar.
    await expect(m.api.get('/pedidos')).rejects.toMatchObject({ status: 401 })
    expect(refreshes).toBe(1)
  })

  it('boot: restaura a sessão pelo cookie; sem cookie vira visitante', async () => {
    servidorFalso({
      'POST /api/auth/refresh': () => ({ status: 200, corpo: { access_token: 'boot', expira_em: 600 } }),
      'GET /api/auth/eu': eu,
    })
    let m = await moduloNovo()
    expect(m.estadoSessao()).toEqual({ status: 'carregando' })
    await m.iniciarSessao()
    expect(m.estadoSessao()).toEqual({ status: 'logado', usuario: ana })

    servidorFalso({ 'POST /api/auth/refresh': () => ({ status: 401, corpo: { erro: 'sessao_invalida' } }) })
    m = await moduloNovo()
    await m.iniciarSessao()
    expect(m.estadoSessao()).toEqual({ status: 'anonimo' })
  })

  it('token vencido é renovado antes da chamada', async () => {
    let refreshes = 0
    servidorFalso({
      'POST /api/auth/login': () => ({ status: 200, corpo: { access_token: 'curto', expira_em: 10 } }),
      'POST /api/auth/refresh': () => {
        refreshes++
        return { status: 200, corpo: { access_token: 'longo', expira_em: 600 } }
      },
      'GET /api/auth/eu': eu,
      'GET /api/pedidos': (init) => ({ status: 200, corpo: { auth: bearer(init) } }),
    })
    const m = await moduloNovo()
    await m.entrar('ana@exemplo.com', 'segredo123') // 10 s < margem de 30 s
    await expect(m.api.get('/pedidos')).resolves.toEqual({ auth: 'Bearer longo' })
    expect(refreshes).toBe(1)
  })

  it('logout numa aba desloga a outra (BroadcastChannel)', async () => {
    servidorFalso({
      'POST /api/auth/login': () => ({ status: 200, corpo: { access_token: 'tok', expira_em: 600 } }),
      'GET /api/auth/eu': eu,
      'POST /api/auth/refresh/logout': () => ({ status: 204 }),
    })
    const abaA = await moduloNovo()
    const abaB = await moduloNovo()
    await abaA.entrar('ana@exemplo.com', 'segredo123')
    await vi.waitFor(() => {
      expect(abaB.estadoSessao()).toEqual({ status: 'logado', usuario: ana })
    })
    await abaA.sair()
    expect(abaA.estadoSessao()).toEqual({ status: 'anonimo' })
    await vi.waitFor(() => {
      expect(abaB.estadoSessao()).toEqual({ status: 'anonimo' })
    })
  })
})
