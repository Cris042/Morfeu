// Sessão da conta no SPA (PRD 0032, refinamentos E1/E8). O access token vive
// só nesta variável de módulo — nunca em storage nem em cookie legível. A
// sessão sobrevive ao recarregar pelo cookie de refresh (HttpOnly, rotativo).
//
// Rotação com detecção de reuso (E1): dois refresh simultâneos com o mesmo
// cookie revogam a família inteira. Por isso o refresh é único: uma promessa
// por aba e, entre abas, o Web Lock "morfeu-refresh" — quem entra no lock
// depois relê o estado e reaproveita o resultado que a outra aba publicou no
// BroadcastChannel. Sem Web Locks sobra o single-flight da aba (se duas abas
// colidirem, a família cai e as duas saem — falha segura).

import { useSyncExternalStore } from 'react'

import { ErroApi, requisitar, usarAutenticador } from './client'
import type { Usuario } from './tipos'

export type EstadoSessao = { status: 'carregando' } | { status: 'anonimo' } | { status: 'logado'; usuario: Usuario }

/** Resultado de um login/refresh: o que as abas compartilham. */
export interface Credencial {
  token: string
  /** Instante (ms) em que o access token expira. */
  expiraEm: number
  usuario: Usuario
}

// ---------------------------------------------------------------------------
// Refresh único (puro, dependências injetadas — testável sem navegador)

export interface DepsRefresh {
  /** Chama o servidor. null = sessão inválida (401); erro = falha transitória. */
  renovar: () => Promise<Credencial | null>
  /** Exclusão entre abas (Web Locks); ausente = só a promessa única da aba. */
  travar?: (fn: () => Promise<Credencial | null>) => Promise<Credencial | null>
  /** Muda a cada resultado recebido de outra aba. */
  geracao: () => number
  /** Credencial vigente nesta aba (relida dentro do lock). */
  atual: () => Credencial | undefined
}

export function criarRefreshUnico(deps: DepsRefresh): () => Promise<Credencial | null> {
  let emVoo: Promise<Credencial | null> | undefined
  return () => {
    if (emVoo) {
      return emVoo
    }
    const antes = deps.geracao()
    const executar = () => {
      // Outra aba renovou enquanto esperávamos o lock: o cookie já girou e o
      // resultado chegou pelo canal — nada a pedir ao servidor.
      const atual = deps.atual()
      if (deps.geracao() !== antes && atual) {
        return Promise.resolve(atual)
      }
      return deps.renovar()
    }
    emVoo = (deps.travar ? deps.travar(executar) : executar()).finally(() => {
      emVoo = undefined
    })
    return emVoo
  }
}

// ---------------------------------------------------------------------------
// Estado da aba

// Margem para não mandar um token que expira no caminho.
const MARGEM_MS = 30_000
const NOME_LOCK = 'morfeu-refresh'
const NOME_CANAL = 'morfeu-sessao'

type Mensagem = { tipo: 'sessao'; credencial: Credencial } | { tipo: 'saiu' }

let estado: EstadoSessao = { status: 'carregando' }
let credencial: Credencial | undefined
let geracao = 0
const ouvintes = new Set<() => void>()
const canal = typeof BroadcastChannel === 'undefined' ? undefined : new BroadcastChannel(NOME_CANAL)

function definir(c: Credencial | undefined) {
  credencial = c
  estado = c ? { status: 'logado', usuario: c.usuario } : { status: 'anonimo' }
  for (const ouvinte of ouvintes) {
    ouvinte()
  }
}

function publicar(m: Mensagem) {
  canal?.postMessage(m)
}

if (canal) {
  canal.onmessage = (ev: MessageEvent<Mensagem>) => {
    geracao++
    definir(ev.data.tipo === 'sessao' ? ev.data.credencial : undefined)
  }
}

interface RespostaSessao {
  access_token: string
  expira_em: number
}

async function credencialDe(r: RespostaSessao): Promise<Credencial> {
  const expiraEm = Date.now() + r.expira_em * 1000
  const usuario = await requisitar<Usuario>('GET', '/auth/eu', undefined, { token: r.access_token })
  return { token: r.access_token, expiraEm, usuario }
}

async function renovarNoServidor(): Promise<Credencial | null> {
  let r: RespostaSessao
  try {
    r = await requisitar<RespostaSessao>('POST', '/auth/refresh', undefined, { autenticar: false })
  } catch (e) {
    if (e instanceof ErroApi && e.status === 401) {
      return null
    }
    throw e
  }
  return credencialDe(r)
}

function travarEntreAbas(): DepsRefresh['travar'] {
  if (typeof navigator === 'undefined' || !('locks' in navigator)) {
    return undefined
  }
  return (fn) => navigator.locks.request(NOME_LOCK, fn)
}

const refreshUnico = criarRefreshUnico({
  renovar: renovarNoServidor,
  travar: travarEntreAbas(),
  geracao: () => geracao,
  atual: () => credencial,
})

/** Renova e aplica o resultado nesta aba e nas demais. */
async function renovar(): Promise<Credencial | undefined> {
  const antes = credencial
  const c = await refreshUnico()
  if (c === null) {
    if (antes) {
      publicar({ tipo: 'saiu' })
    }
    definir(undefined)
    return undefined
  }
  // c === credencial: resultado que outra aba já publicou (nada a repassar).
  if (c !== credencial) {
    definir(c)
    publicar({ tipo: 'sessao', credencial: c })
  }
  return c
}

usarAutenticador({
  token: async () => {
    if (!credencial) {
      return undefined
    }
    if (credencial.expiraEm - MARGEM_MS > Date.now()) {
      return credencial.token
    }
    return (await renovar().catch(() => undefined))?.token
  },
  renovar: async () => (await renovar().catch(() => undefined))?.token,
})

// ---------------------------------------------------------------------------
// API pública

/** Restaura a sessão no boot pelo cookie de refresh. */
export async function iniciarSessao(): Promise<void> {
  try {
    await renovar()
  } catch {
    // Rede/5xx no boot: segue como visitante; o próximo login resolve.
    definir(undefined)
  }
}

export async function entrar(email: string, senha: string): Promise<void> {
  const r = await requisitar<RespostaSessao>('POST', '/auth/login', { email, senha }, { autenticar: false })
  const c = await credencialDe(r)
  definir(c)
  publicar({ tipo: 'sessao', credencial: c })
}

export async function cadastrar(nome: string, email: string, senha: string): Promise<void> {
  await requisitar('POST', '/auth/registro', { nome, email, senha }, { autenticar: false })
  await entrar(email, senha)
}

export async function sair(): Promise<void> {
  try {
    await requisitar('POST', '/auth/refresh/logout', undefined, { autenticar: false })
  } finally {
    // Mesmo com falha de rede a aba esquece o token; o cookie expira sozinho.
    definir(undefined)
    publicar({ tipo: 'saiu' })
  }
}

export function estadoSessao(): EstadoSessao {
  return estado
}

export function assinarSessao(ouvinte: () => void): () => void {
  ouvintes.add(ouvinte)
  return () => ouvintes.delete(ouvinte)
}

export function useSessao(): EstadoSessao {
  return useSyncExternalStore(assinarSessao, estadoSessao)
}
