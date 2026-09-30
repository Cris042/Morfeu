// Cliente HTTP único da SPA (ADR 0009, PRD 0017 RF04). Todo acesso à API
// passa por aqui: base /api (o proxy remove o prefixo), cookie sempre
// (credentials: 'include') e o header anti-CSRF exigido pela API nas escritas.
// Logado, manda o Bearer; um 401 dispara um refresh e 1 nova tentativa
// (PRD 0032 — o token vem do módulo de sessão, injetado para evitar ciclo).

const BASE = '/api'
const HEADER_ANTI_CSRF = 'X-Requested-With'
const VALOR_ANTI_CSRF = 'morfeu'

/** Erro de uma chamada à API: status 0 = falha de rede. */
export class ErroApi extends Error {
  readonly status: number
  /** Campo "erro" do corpo JSON (ex.: "assento_indisponivel"), se houver. */
  readonly codigo: string | undefined
  readonly corpo: unknown

  constructor(status: number, codigo: string | undefined, corpo: unknown) {
    super(codigo ? `API ${String(status)}: ${codigo}` : `API ${String(status)}`)
    this.name = 'ErroApi'
    this.status = status
    this.codigo = codigo
    this.corpo = corpo
  }
}

type Metodo = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

async function lerCorpo(resposta: Response): Promise<unknown> {
  const texto = await resposta.text()
  if (texto === '') {
    return undefined
  }
  try {
    return JSON.parse(texto) as unknown
  } catch {
    return texto
  }
}

function codigoDe(corpo: unknown): string | undefined {
  if (typeof corpo === 'object' && corpo !== null && 'erro' in corpo) {
    const erro = corpo.erro
    return typeof erro === 'string' ? erro : undefined
  }
  return undefined
}

/** Fonte do access token (src/api/sessao.ts). */
export interface Autenticador {
  /** Token válido agora (renova se expirou), ou undefined se deslogado. */
  token: () => Promise<string | undefined>
  /** Renova depois de um 401; undefined = sessão encerrada (sem nova tentativa). */
  renovar: () => Promise<string | undefined>
}

let autenticador: Autenticador | undefined

export function usarAutenticador(a: Autenticador | undefined) {
  autenticador = a
}

export interface Opcoes {
  /** false: nunca manda Bearer nem renova (rotas /auth/*). */
  autenticar?: boolean
  /** Bearer explícito (ex.: GET /auth/eu logo após o login). */
  token?: string
}

async function enviar(metodo: Metodo, caminho: string, corpo: unknown, token: string | undefined): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (metodo !== 'GET') {
    headers[HEADER_ANTI_CSRF] = VALOR_ANTI_CSRF
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  // no-referrer: a API nunca precisa do Referer, e a página do ingresso tem
  // o token no path (PRD 0035).
  const init: RequestInit = { method: metodo, credentials: 'include', headers, referrerPolicy: 'no-referrer' }
  if (corpo !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(corpo)
  }
  try {
    return await fetch(BASE + caminho, init)
  } catch (causa) {
    throw new ErroApi(0, 'rede', causa)
  }
}

export async function requisitar<T>(metodo: Metodo, caminho: string, corpo?: unknown, opcoes: Opcoes = {}): Promise<T> {
  // /auth/* nunca leva o Bearer automático (nem renova): imposto pelo caminho.
  const aut = opcoes.autenticar === false || caminho.startsWith('/auth/') ? undefined : autenticador
  const token = opcoes.token ?? (await aut?.token())
  let resposta = await enviar(metodo, caminho, corpo, token)
  // Só um 401 de requisição que levou o token automático renova — e uma vez.
  if (resposta.status === 401 && token && aut && !opcoes.token) {
    const novo = await aut.renovar()
    if (novo) {
      resposta = await enviar(metodo, caminho, corpo, novo)
    }
  }
  const dados = await lerCorpo(resposta)
  if (!resposta.ok) {
    throw new ErroApi(resposta.status, codigoDe(dados), dados)
  }
  return dados as T
}

export const api = {
  get: <T>(caminho: string) => requisitar<T>('GET', caminho),
  post: <T>(caminho: string, corpo?: unknown) => requisitar<T>('POST', caminho, corpo),
  delete: (caminho: string) => requisitar<undefined>('DELETE', caminho),
}
