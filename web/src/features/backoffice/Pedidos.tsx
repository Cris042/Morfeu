import { useState } from 'react'
import type { SubmitEvent } from 'react'
import { Link, useParams } from 'react-router'

import { PEDIDOS_OPERADOR_POR_PAGINA, useCancelarPedidoOperador, usePedidoOperador, usePedidosOperador } from '../../api/backoffice'
import type { FiltroPedidos } from '../../api/backoffice'
import { ErroApi } from '../../api/client'
import { naoEncontrado } from '../../api/consultas'
import type { StatusPedido } from '../../api/tipos'
import { Carregando, Falha, Vazio } from '../../ui/Estado'
import { dia, hora, preco } from '../../ui/formato'
import { ROTULOS } from '../conta/MeusPedidos'
import styles from './Backoffice.module.css'

const STATUS = Object.keys(ROTULOS) as StatusPedido[]

const MOTIVOS: Record<string, string> = {
  divergencia: 'valor divergente',
  tardio: 'pagamento após o prazo',
  emissao: 'assento perdido',
  cancelamento: 'cancelado pelo cliente',
  operador: 'cancelado pela operação',
  sessao_cancelada: 'sessão cancelada',
}

/** Pedidos do operador (PRD 0039): filtros por sessão e situação. */
export function Pedidos() {
  const [filtro, setFiltro] = useState<FiltroPedidos>({})
  const [pagina, setPagina] = useState(1)
  const pedidos = usePedidosOperador(filtro, pagina)

  function filtrar(ev: SubmitEvent<HTMLFormElement>) {
    ev.preventDefault()
    const d = new FormData(ev.currentTarget)
    const sessao = d.get('sessao')
    const status = d.get('status')
    setFiltro({
      sessao: typeof sessao === 'string' && sessao !== '' ? Number(sessao) : undefined,
      status: typeof status === 'string' && status !== '' ? (status as StatusPedido) : undefined,
    })
    setPagina(1)
  }

  return (
    <>
      <form className={styles.linha} onSubmit={filtrar} aria-label="Filtrar pedidos">
        <label className={styles.campo}>
          Sessão (nº)
          <input name="sessao" inputMode="numeric" pattern="[0-9]+" />
        </label>
        <label className={styles.campo}>
          Situação
          <select name="status">
            <option value="">Todas</option>
            {STATUS.map((s) => (
              <option key={s} value={s}>
                {ROTULOS[s]}
              </option>
            ))}
          </select>
        </label>
        <button type="submit" className={styles.secundario}>
          Filtrar
        </button>
      </form>
      {pedidos.isPending && <Carregando texto="Buscando pedidos…" />}
      {pedidos.isError && <Falha texto="Não foi possível carregar os pedidos." aoTentar={() => void pedidos.refetch()} />}
      {pedidos.isSuccess && pedidos.data.pedidos.length === 0 && <Vazio>Nenhum pedido com esses filtros.</Vazio>}
      {pedidos.isSuccess && pedidos.data.pedidos.length > 0 && (
        <ul className={styles.lista}>
          {pedidos.data.pedidos.map((p) => (
            <li key={p.id} className={styles.item}>
              <Link to={`/backoffice/pedidos/${p.id}`} className={styles.nome}>
                {p.email} — sessão {p.sessao_id}
              </Link>
              <span className={styles.meta}>
                {ROTULOS[p.status]} · {p.assentos.join(', ')} · {preco(p.total_centavos)}
              </span>
            </li>
          ))}
        </ul>
      )}
      <nav className={styles.linha} aria-label="Páginas de pedidos">
        {pagina > 1 && (
          <button type="button" className={styles.secundario} onClick={() => { setPagina(pagina - 1) }}>
            Mais recentes
          </button>
        )}
        {pedidos.data?.pedidos.length === PEDIDOS_OPERADOR_POR_PAGINA && (
          <button type="button" className={styles.secundario} onClick={() => { setPagina(pagina + 1) }}>
            Mais antigos
          </button>
        )}
      </nav>
    </>
  )
}

function mensagemDe(erro: unknown): string {
  if (erro instanceof ErroApi) {
    if (erro.codigo === 'sessao_iniciada') {
      return 'A sessão já começou: o pedido não pode mais ser cancelado.'
    }
    if (erro.codigo === 'nao_cancelavel') {
      return 'Só pedido pago, sem ingresso usado, pode ser cancelado.'
    }
  }
  return 'Não foi possível cancelar o pedido.'
}

/** Detalhe do pedido para o operador, com o cancelamento sem janela. */
export function PedidoDoOperador() {
  const { id = '' } = useParams()
  const consulta = usePedidoOperador(id)
  const cancelar = useCancelarPedidoOperador(id)
  const [confirmando, setConfirmando] = useState(false)
  const [erro, setErro] = useState('')

  if (consulta.isPending) {
    return <Carregando texto="Buscando o pedido…" />
  }
  if (consulta.isError) {
    if (naoEncontrado(consulta.error)) {
      return <Vazio>Pedido não encontrado.</Vazio>
    }
    return <Falha texto="Não foi possível carregar o pedido." aoTentar={() => void consulta.refetch()} />
  }
  const { pedido: p, ingressos, eventos } = consulta.data

  async function confirmar() {
    setErro('')
    try {
      await cancelar.mutateAsync()
      setConfirmando(false)
    } catch (e) {
      setErro(mensagemDe(e))
    }
  }

  return (
    <section aria-labelledby="titulo-pedido-op">
      <h2 id="titulo-pedido-op" className={styles.subtitulo}>
        Pedido de {p.email}
      </h2>
      <dl className={styles.lista}>
        <dt>Situação</dt>
        <dd>
          {ROTULOS[p.status]}
          {p.motivo_estorno && ` (${MOTIVOS[p.motivo_estorno] ?? p.motivo_estorno})`}
        </dd>
        <dt>Sessão</dt>
        <dd>{p.sessao_id}</dd>
        <dt>Assentos</dt>
        <dd>{ingressos.length > 0 ? ingressos.map((i) => `${i.assento} (${i.status})`).join(', ') : p.assentos.join(', ')}</dd>
        <dt>Total</dt>
        <dd>{preco(p.total_centavos)}</dd>
      </dl>
      <h3 className={styles.subtitulo}>Histórico</h3>
      <ol className={styles.lista}>
        {eventos.map((e) => (
          <li key={`${e.para}-${e.ocorrido_em}`} className={styles.meta}>
            {dia(e.ocorrido_em)} {hora(e.ocorrido_em)} — {e.de ? `${ROTULOS[e.de as StatusPedido]} → ` : ''}
            {ROTULOS[e.para as StatusPedido]}
          </li>
        ))}
      </ol>
      {p.status === 'pago' &&
        (confirmando ? (
          <div role="group" aria-label="Confirmar cancelamento do pedido" className={styles.linha}>
            <span>O valor será estornado e os ingressos deixam de valer.</span>
            <button type="button" className={styles.primario} disabled={cancelar.isPending} onClick={() => void confirmar()}>
              Confirmar cancelamento
            </button>
            <button type="button" className={styles.secundario} onClick={() => { setConfirmando(false) }}>
              Manter pedido
            </button>
          </div>
        ) : (
          <button type="button" className={styles.secundario} onClick={() => { setConfirmando(true) }}>
            Cancelar pedido
          </button>
        ))}
      <p className={styles.erro} role="alert">
        {erro}
      </p>
      <p>
        <Link to="/backoffice/pedidos">Voltar aos pedidos</Link>
      </p>
    </section>
  )
}
