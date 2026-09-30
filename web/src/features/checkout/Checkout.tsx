import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { SubmitEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router'

import { criarPedido, retomarPedido } from '../../api/checkout'
import { ErroApi } from '../../api/client'
import { useMeusHolds } from '../../api/consultas'
import { useSessao } from '../../api/sessao'
import { Carregando, Falha } from '../../ui/Estado'
import { hora, preco } from '../../ui/formato'
import { useAgora } from '../../ui/useAgora'
import styles from './Checkout.module.css'
import { Pagamento } from './Pagamento'

interface EmPagamento {
  pedidoId: string
  codigo: string
  totalCentavos: number
  expiraEm: string
  segredo: string
}

interface Aviso {
  texto: string
  /** Assentos perdidos: o caminho é voltar ao mapa. */
  voltarAoMapa?: boolean
}

function avisoDe(erro: unknown): Aviso {
  if (erro instanceof ErroApi) {
    if (erro.codigo === 'holds_invalidos') {
      return { texto: 'Seus assentos expiraram ou foram liberados. Escolha de novo.', voltarAoMapa: true }
    }
    if (erro.codigo === 'dados_invalidos') {
      return { texto: 'Informe um e-mail válido para receber os ingressos.' }
    }
    if (erro.codigo === 'pagamento_indisponivel') {
      return { texto: 'O pagamento está indisponível agora. Seus assentos continuam reservados; tente de novo em instantes.' }
    }
    if (erro.status === 429) {
      return { texto: 'Muitas tentativas seguidas. Espere um minuto e tente de novo.' }
    }
    if (erro.status === 404) {
      return { texto: 'O pedido anterior não pode mais ser pago. Escolha os assentos de novo.', voltarAoMapa: true }
    }
  }
  return { texto: 'Não conseguimos abrir o pedido agora. Tente de novo em instantes.' }
}

function pedidoPendente(erro: unknown): string | undefined {
  if (erro instanceof ErroApi && erro.codigo === 'pedido_pendente') {
    const id = (erro.corpo as { pedido_id?: unknown } | undefined)?.pedido_id
    return typeof id === 'string' ? id : undefined
  }
  return undefined
}

/** Checkout da sessão (PRD 0033): e-mail → pedido → pagamento → acompanhamento. */
export function Checkout() {
  const { id } = useParams()
  const sessaoID = Number(id)
  if (!Number.isSafeInteger(sessaoID) || sessaoID <= 0) {
    return (
      <section>
        <h1>Esta sessão não está mais disponível</h1>
        <p>
          <Link to="/">Voltar ao cartaz</Link>
        </p>
      </section>
    )
  }
  return <Fluxo sessaoID={sessaoID} />
}

function Fluxo({ sessaoID }: { sessaoID: number }) {
  const sessao = useSessao()
  const holds = useMeusHolds()
  const cliente = useQueryClient()
  const navegar = useNavigate()
  const [pagamento, setPagamento] = useState<EmPagamento>()
  const [aviso, setAviso] = useState<Aviso>()
  const [enviando, setEnviando] = useState(false)
  const agora = useAgora(pagamento === undefined)

  if (pagamento) {
    return (
      <section className={styles.checkout} aria-labelledby="titulo-checkout">
        <h1 id="titulo-checkout" className={styles.titulo}>
          Pagamento
        </h1>
        <dl className={styles.resumo}>
          <dt>Pedido</dt>
          <dd className={styles.codigo}>{pagamento.codigo}</dd>
          <dt>Total</dt>
          <dd>{preco(pagamento.totalCentavos)}</dd>
          <dt>Pague até</dt>
          <dd>
            <time dateTime={pagamento.expiraEm}>{hora(pagamento.expiraEm)}</time>
          </dd>
        </dl>
        <Pagamento
          pedidoId={pagamento.pedidoId}
          segredo={pagamento.segredo}
          aoConcluir={() => {
            void navegar(`/pedido/${pagamento.pedidoId}`, { replace: true })
          }}
        />
      </section>
    )
  }

  if (holds.isPending) {
    return <Carregando texto="Conferindo seus assentos…" />
  }
  if (holds.isError) {
    return <Falha texto="Não conseguimos conferir seus assentos." aoTentar={() => void holds.refetch()} />
  }
  const assentos = holds.data
    .filter((h) => h.sessao_id === sessaoID && Date.parse(h.expira_em) > agora)
    .map((h) => h.assento)
    .sort()

  async function continuar(ev: SubmitEvent<HTMLFormElement>) {
    ev.preventDefault()
    const email = new FormData(ev.currentTarget).get('email')
    setEnviando(true)
    setAviso(undefined)
    try {
      const r = await criarPedido(typeof email === 'string' ? email.trim() : '', sessaoID, assentos)
      setPagamento({
        pedidoId: r.pedido.id,
        codigo: r.pedido.codigo,
        totalCentavos: r.pedido.total_centavos,
        expiraEm: r.pedido.expira_em,
        segredo: r.client_secret,
      })
    } catch (erro) {
      const pendente = pedidoPendente(erro)
      if (pendente) {
        await retomar(pendente)
      } else {
        mostrar(erro)
      }
    } finally {
      setEnviando(false)
    }
  }

  // 409 pedido_pendente: o carrinho já tem um pedido aberto — retoma o
  // pagamento dele em vez de criar outro (PRD 0031).
  async function retomar(pedidoId: string) {
    try {
      const r = await retomarPedido(pedidoId)
      setPagamento({
        pedidoId,
        codigo: r.codigo,
        totalCentavos: r.total_centavos,
        expiraEm: r.expira_em,
        segredo: r.client_secret,
      })
    } catch (erro) {
      mostrar(erro)
    }
  }

  function mostrar(erro: unknown) {
    const a = avisoDe(erro)
    setAviso(a)
    if (a.voltarAoMapa) {
      void cliente.invalidateQueries({ queryKey: ['holds'] })
    }
  }

  if (assentos.length === 0 && !aviso) {
    return (
      <section className={styles.checkout}>
        <h1 className={styles.titulo}>Nenhum assento reservado</h1>
        <p>
          Seus assentos expiraram ou foram liberados.{' '}
          <Link to={`/sessoes/${String(sessaoID)}`}>Escolher assentos</Link>
        </p>
      </section>
    )
  }

  const emailDaConta = sessao.status === 'logado' ? sessao.usuario.email : ''
  return (
    <section className={styles.checkout} aria-labelledby="titulo-checkout">
      <h1 id="titulo-checkout" className={styles.titulo}>
        Finalizar compra
      </h1>
      <p>
        Assentos <span className={styles.codigo}>{assentos.join(', ')}</span>
      </p>
      <form className={styles.formulario} onSubmit={(ev) => void continuar(ev)}>
        <label className={styles.campo}>
          E-mail para receber os ingressos
          {/* key: a sessão restaurada depois do 1º render ainda preenche o campo. */}
          <input
            key={emailDaConta}
            name="email"
            type="email"
            autoComplete="email"
            required
            maxLength={254}
            defaultValue={emailDaConta}
          />
        </label>
        {sessao.status === 'anonimo' && (
          <p className={styles.nota}>
            Compra como convidado. Tem conta? <Link to={`/entrar?volta=/sessoes/${String(sessaoID)}/pagamento`}>Entrar</Link>
          </p>
        )}
        <p className={styles.erro} role="alert">
          {aviso?.texto}
        </p>
        {aviso?.voltarAoMapa ? (
          <Link to={`/sessoes/${String(sessaoID)}`} className={styles.primario}>
            Escolher assentos
          </Link>
        ) : (
          <button type="submit" className={styles.primario} disabled={enviando || assentos.length === 0}>
            {enviando ? 'Abrindo o pedido…' : 'Ir para o pagamento'}
          </button>
        )}
      </form>
    </section>
  )
}
