import { useState } from 'react'
import type { SubmitEvent } from 'react'
import { Link } from 'react-router'

import { ErroApi } from '../../api/client'
import { cancelarPorConsulta, consultarPedido } from '../../api/consulta'
import type { ResultadoConsulta } from '../../api/consulta'
import { CancelarPedido } from '../../ui/CancelarPedido'
import { preco } from '../../ui/formato'
import { ROTULOS } from '../conta/MeusPedidos'
import styles from './Consulta.module.css'

function mensagemDe(erro: unknown): string {
  if (erro instanceof ErroApi) {
    if (erro.status === 404) {
      // Mensagem única: não diz se o e-mail ou o código está errado.
      return 'Não encontramos um pedido com esse e-mail e código. Confira os dados do e-mail de confirmação.'
    }
    if (erro.status === 429) {
      return 'Muitas consultas seguidas. Espere um minuto e tente de novo.'
    }
  }
  return 'Não conseguimos consultar agora. Tente de novo em instantes.'
}

/** Consulta de convidado (PRD 0035): e-mail + código do pedido. */
export function Consulta() {
  const [resultado, setResultado] = useState<ResultadoConsulta>()
  // As credenciais da consulta ficam só em memória (nunca em storage) para o
  // cancelamento (PRD 0038).
  const [credenciais, setCredenciais] = useState<{ email: string; codigo: string }>()
  const [erro, setErro] = useState('')
  const [enviando, setEnviando] = useState(false)

  async function consultar(ev: SubmitEvent<HTMLFormElement>) {
    ev.preventDefault()
    const dados = new FormData(ev.currentTarget)
    const texto = (c: string) => {
      const v = dados.get(c)
      return typeof v === 'string' ? v : ''
    }
    setEnviando(true)
    setErro('')
    setResultado(undefined)
    setCredenciais(undefined)
    try {
      const email = texto('email')
      const codigo = texto('codigo')
      setResultado(await consultarPedido(email, codigo))
      setCredenciais({ email, codigo })
    } catch (e) {
      setErro(mensagemDe(e))
    } finally {
      setEnviando(false)
    }
  }

  return (
    <section className={styles.consulta} aria-labelledby="titulo-consulta">
      <h1 id="titulo-consulta" className={styles.titulo}>
        Consultar pedido
      </h1>
      <p className={styles.nota}>Comprou sem conta? Use o e-mail da compra e o código do pedido que enviamos.</p>
      <form className={styles.formulario} onSubmit={(ev) => void consultar(ev)}>
        <label className={styles.campo}>
          E-mail
          <input name="email" type="email" autoComplete="email" required maxLength={254} />
        </label>
        <label className={styles.campo}>
          Código do pedido
          <input name="codigo" autoComplete="off" autoCapitalize="characters" spellCheck={false} required maxLength={24} />
        </label>
        <p className={styles.erro} role="alert">
          {erro}
        </p>
        <button type="submit" className={styles.primario} disabled={enviando}>
          {enviando ? 'Consultando…' : 'Consultar'}
        </button>
      </form>
      {resultado && (
        <Resultado
          r={resultado}
          aoCancelar={
            credenciais &&
            (async () => {
              setResultado(await cancelarPorConsulta(credenciais.email, credenciais.codigo))
            })
          }
        />
      )}
    </section>
  )
}

function Resultado({ r, aoCancelar }: { r: ResultadoConsulta; aoCancelar?: () => Promise<void> }) {
  const p = r.pedido
  return (
    <section className={styles.resultado} aria-labelledby="titulo-resultado">
      <h2 id="titulo-resultado">
        Pedido <span className={styles.codigo}>{p.codigo}</span>
      </h2>
      <dl className={styles.resumo}>
        <dt>Situação</dt>
        <dd>{ROTULOS[p.status]}</dd>
        <dt>Assentos</dt>
        <dd>{p.assentos.join(', ')}</dd>
        <dt>Total</dt>
        <dd>{preco(p.total_centavos)}</dd>
      </dl>
      {r.ingressos.length > 0 ? (
        <ul className={styles.ingressos}>
          {r.ingressos.map((i) => (
            <li key={i.ref}>
              <Link to={`/i/${i.ref}`}>Ingresso {i.assento}</Link>
            </li>
          ))}
        </ul>
      ) : (
        <p className={styles.nota}>
          {p.status === 'estorno_pendente' || p.status === 'estornado'
            ? 'Pedido cancelado: o valor volta ao meio de pagamento em alguns dias.'
            : 'Os ingressos aparecem aqui quando o pagamento é confirmado.'}
        </p>
      )}
      {p.cancelavel && aoCancelar && <CancelarPedido aoCancelar={aoCancelar} />}
    </section>
  )
}
