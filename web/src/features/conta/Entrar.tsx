import { useState } from 'react'
import type { SubmitEvent } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router'

import { ErroApi } from '../../api/client'
import { cadastrar, entrar, useSessao } from '../../api/sessao'
import styles from './Conta.module.css'

const DESTINO_PADRAO = '/conta/pedidos'

/** Só caminhos locais: "?volta=//outro.site" não vira redirecionamento aberto. */
export function destinoSeguro(volta: string | null): string {
  if (!volta?.startsWith('/') || volta.startsWith('//') || volta.startsWith('/\\')) {
    return DESTINO_PADRAO
  }
  return volta
}

const CAMPOS: Record<string, string> = {
  nome: 'Informe seu nome (até 120 caracteres).',
  email: 'Informe um e-mail válido.',
  senha: 'A senha precisa ter de 8 a 128 caracteres.',
}

function mensagemDe(erro: unknown, modo: Modo): string {
  if (erro instanceof ErroApi) {
    if (erro.codigo === 'credenciais_invalidas') {
      return 'E-mail ou senha incorretos.'
    }
    if (erro.codigo === 'email_em_uso') {
      return 'Este e-mail já tem conta. Entre com ele.'
    }
    if (erro.status === 429) {
      return 'Muitas tentativas. Aguarde alguns minutos e tente de novo.'
    }
    if (erro.codigo === 'dados_invalidos') {
      const corpo = erro.corpo as { campos?: string[] } | undefined
      const campos = (corpo?.campos ?? []).map((c) => CAMPOS[c]).filter(Boolean)
      if (campos.length > 0) {
        return campos.join(' ')
      }
    }
  }
  return modo === 'entrar' ? 'Não foi possível entrar agora. Tente de novo.' : 'Não foi possível criar a conta agora. Tente de novo.'
}

type Modo = 'entrar' | 'cadastro'

/** Login e cadastro (PRD 0032): o cadastro já entra na conta criada. */
export function Entrar({ modo }: { modo: Modo }) {
  const sessao = useSessao()
  const navegar = useNavigate()
  const [params] = useSearchParams()
  const destino = destinoSeguro(params.get('volta'))
  const [erro, setErro] = useState<string>()
  const [enviando, setEnviando] = useState(false)

  if (sessao.status === 'logado' && !enviando) {
    return <Navigate to={destino} replace />
  }

  async function enviar(ev: SubmitEvent<HTMLFormElement>) {
    ev.preventDefault()
    const dados = new FormData(ev.currentTarget)
    const texto = (campo: string) => {
      const v = dados.get(campo)
      return typeof v === 'string' ? v : ''
    }
    setEnviando(true)
    setErro(undefined)
    try {
      if (modo === 'entrar') {
        await entrar(texto('email'), texto('senha'))
      } else {
        await cadastrar(texto('nome'), texto('email'), texto('senha'))
      }
      void navegar(destino, { replace: true })
    } catch (e) {
      setErro(mensagemDe(e, modo))
    } finally {
      setEnviando(false)
    }
  }

  const titulo = modo === 'entrar' ? 'Entrar' : 'Criar conta'
  const volta = params.get('volta')
  const sufixo = volta ? `?volta=${encodeURIComponent(destino)}` : ''
  return (
    <section className={styles.conta}>
      <h1 className={styles.titulo}>{titulo}</h1>
      <form className={styles.formulario} onSubmit={(ev) => void enviar(ev)}>
        {modo === 'cadastro' && (
          <label className={styles.campo}>
            Nome
            <input name="nome" autoComplete="name" required maxLength={120} />
          </label>
        )}
        <label className={styles.campo}>
          E-mail
          <input name="email" type="email" autoComplete="email" required maxLength={254} />
        </label>
        <label className={styles.campo}>
          Senha
          <input
            name="senha"
            type="password"
            autoComplete={modo === 'entrar' ? 'current-password' : 'new-password'}
            required
            minLength={modo === 'cadastro' ? 8 : undefined}
            maxLength={128}
          />
        </label>
        <p className={styles.erro} role="alert">
          {erro}
        </p>
        <button type="submit" className={styles.primario} disabled={enviando}>
          {enviando ? 'Aguarde…' : titulo}
        </button>
      </form>
      <p className={styles.alternativa}>
        {modo === 'entrar' ? (
          <>
            Ainda não tem conta? <Link to={`/cadastro${sufixo}`}>Criar conta</Link>
          </>
        ) : (
          <>
            Já tem conta? <Link to={`/entrar${sufixo}`}>Entrar</Link>
          </>
        )}
      </p>
      <p className={styles.nota}>A conta é opcional: sem ela, você compra só com o e-mail.</p>
    </section>
  )
}
