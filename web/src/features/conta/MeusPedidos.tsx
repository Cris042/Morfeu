import { useState } from 'react'
import type { ReactNode } from 'react'
import { Link, Navigate, useLocation, useParams } from 'react-router'

import { naoEncontrado } from '../../api/consultas'
import { PEDIDOS_POR_PAGINA, useCancelarDaConta, useMeusPedidos, usePedidoDaConta } from '../../api/conta'
import { useSessao } from '../../api/sessao'
import type { Pedido, StatusPedido } from '../../api/tipos'
import { CancelarPedido } from '../../ui/CancelarPedido'
import { Carregando, Falha, Vazio } from '../../ui/Estado'
import { hora, preco } from '../../ui/formato'
import styles from './Conta.module.css'

export const ROTULOS: Record<StatusPedido, string> = {
  aguardando_pagamento: 'Aguardando pagamento',
  pago: 'Pago',
  expirado: 'Expirado',
  falhou: 'Pagamento recusado',
  estorno_pendente: 'Estorno em andamento',
  estornado: 'Estornado',
}

/** Rotas da conta: visitante vai ao login e volta para cá depois. */
function ExigirConta({ children }: { children: ReactNode }) {
  const sessao = useSessao()
  const local = useLocation()
  if (sessao.status === 'carregando') {
    return <Carregando />
  }
  if (sessao.status === 'anonimo') {
    return <Navigate to={`/entrar?volta=${encodeURIComponent(local.pathname + local.search)}`} replace />
  }
  return children
}

function assentos(p: Pedido): string {
  return p.assentos.join(', ')
}

function ListaDePedidos() {
  const [pagina, setPagina] = useState(1)
  const consulta = useMeusPedidos(pagina)
  if (consulta.isPending) {
    return <Carregando texto="Buscando seus pedidos…" />
  }
  if (consulta.isError) {
    return <Falha texto="Não foi possível carregar seus pedidos." aoTentar={() => void consulta.refetch()} />
  }
  const pedidos = consulta.data.pedidos
  if (pedidos.length === 0 && pagina === 1) {
    return (
      <Vazio>
        Você ainda não comprou ingressos com esta conta. <Link to="/">Ver o cartaz</Link>
      </Vazio>
    )
  }
  return (
    <>
      <ul className={styles.pedidos}>
        {pedidos.map((p) => (
          <li key={p.id} className={styles.pedido}>
            <Link to={`/conta/pedidos/${p.id}`} className={styles.codigo}>
              Pedido {p.codigo}
            </Link>
            <span className={styles.status}>{ROTULOS[p.status]}</span>
            <span>Assentos {assentos(p)}</span>
            <span>{preco(p.total_centavos)}</span>
          </li>
        ))}
      </ul>
      <nav className={styles.paginas} aria-label="Páginas de pedidos">
        {pagina > 1 && (
          <button type="button" className={styles.secundario} onClick={() => { setPagina(pagina - 1) }}>
            Mais recentes
          </button>
        )}
        {pedidos.length === PEDIDOS_POR_PAGINA && (
          <button type="button" className={styles.secundario} onClick={() => { setPagina(pagina + 1) }}>
            Mais antigos
          </button>
        )}
      </nav>
    </>
  )
}

export function MeusPedidos() {
  return (
    <section className={styles.conta}>
      <h1 className={styles.titulo}>Meus pedidos</h1>
      <ExigirConta>
        <ListaDePedidos />
      </ExigirConta>
    </section>
  )
}

function Detalhe({ id }: { id: string }) {
  const consulta = usePedidoDaConta(id)
  const cancelar = useCancelarDaConta(id)
  if (consulta.isPending) {
    return <Carregando texto="Buscando o pedido…" />
  }
  if (consulta.isError) {
    if (naoEncontrado(consulta.error)) {
      return (
        <Vazio>
          Pedido não encontrado nesta conta. <Link to="/conta/pedidos">Ver meus pedidos</Link>
        </Vazio>
      )
    }
    return <Falha texto="Não foi possível carregar o pedido." aoTentar={() => void consulta.refetch()} />
  }
  const p = consulta.data
  return (
    <>
      <h1 className={styles.titulo}>Pedido {p.codigo}</h1>
      <dl className={styles.detalhe}>
        <dt>Situação</dt>
        <dd>{ROTULOS[p.status]}</dd>
        <dt>Assentos</dt>
        <dd>{assentos(p)}</dd>
        <dt>Total</dt>
        <dd>{preco(p.total_centavos)}</dd>
        {p.status === 'aguardando_pagamento' && (
          <>
            <dt>Pagar até</dt>
            <dd>
              <time dateTime={p.expira_em}>{hora(p.expira_em)}</time>
            </dd>
          </>
        )}
      </dl>
      {p.status === 'pago' && <p>Os ingressos com QR foram enviados ao e-mail do pedido.</p>}
      {p.status === 'estorno_pendente' && <p>Pedido cancelado: o valor volta ao meio de pagamento em alguns dias.</p>}
      {p.cancelavel && <CancelarPedido aoCancelar={() => cancelar.mutateAsync()} />}
      <p>
        <Link to="/conta/pedidos">Voltar aos meus pedidos</Link>
      </p>
    </>
  )
}

export function PedidoDaConta() {
  const { id = '' } = useParams()
  return (
    <section className={styles.conta}>
      <ExigirConta>
        <Detalhe id={id} />
      </ExigirConta>
    </section>
  )
}
