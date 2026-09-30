// Cliente HTTP único da SPA (ADR 0009, PRD 0017 RF04). Todo acesso à API
// passa por aqui: base /api (o proxy remove o prefixo), cookie sempre
// (credentials: 'include') e o header anti-CSRF exigido pela API nas escritas.

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

async function requisitar<T>(metodo: Metodo, caminho: string, corpo?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (metodo !== 'GET') {
    headers[HEADER_ANTI_CSRF] = VALOR_ANTI_CSRF
  }
  const init: RequestInit = { method: metodo, credentials: 'include', headers }
  if (corpo !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(corpo)
  }

  let resposta: Response
  try {
    resposta = await fetch(BASE + caminho, init)
  } catch (causa) {
    throw new ErroApi(0, 'rede', causa)
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
