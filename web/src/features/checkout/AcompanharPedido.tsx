import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router'

import { useAcompanharPedido } from '../../api/checkout'
import { naoEncontrado } from '../../api/consultas'
import type { Pedido } from '../../api/tipos'
import { Carregando, Falha } from '../../ui/Estado'
import { preco } from '../../ui/formato'
import styles from './Checkout.module.css'

/** Depois disso o SPA para de consultar e avisa que o e-mail chegará. */
export const TETO_POLLING_MS = 120_000

/** Acompanha o pedido até o webhook decidir (PRD 0033). */
export function AcompanharPedido() {
  const { id = '' } = useParams()
  const [esgotou, setEsgotou] = useState(false)
  const [rodada, setRodada] = useState(0)
  const consulta = useAcompanharPedido(id, !esgotou)
  const cliente = useQueryClient()
  const status = consulta.data?.status

  useEffect(() => {
    const t = setTimeout(() => {
      setEsgotou(true)
    }, TETO_POLLING_MS)
    return () => {
      clearTimeout(t)
    }
  }, [rodada])

  // Pago: os holds viraram ingressos — o painel "Seus assentos" some.
  useEffect(() => {
    if (status === 'pago') {
      void cliente.invalidateQueries({ queryKey: ['holds'] })
    }
  }, [status, cliente])

  if (consulta.isPending) {
    return <Carregando texto="Buscando o pedido…" />
  }
  if (consulta.isError) {
    if (naoEncontrado(consulta.error)) {
      return (
        <section className={styles.checkout}>
          <h1 className={styles.titulo}>Pedido não encontrado neste navegador</h1>
          <p>Os ingressos de um pedido pago chegam no e-mail informado na compra.</p>
        </section>
      )
    }
    return <Falha texto="Não conseguimos consultar o pedido." aoTentar={() => void consulta.refetch()} />
  }
  return (
    <section className={styles.checkout} aria-labelledby="titulo-pedido">
      <div role="status" aria-live="polite">
        <Situacao
          pedido={consulta.data}
          esgotou={esgotou}
          aoVerificar={() => {
            setEsgotou(false)
            setRodada((r) => r + 1)
            void consulta.refetch()
          }}
        />
      </div>
      <dl className={styles.resumo}>
        <dt>Pedido</dt>
        <dd className={styles.codigo}>{consulta.data.codigo}</dd>
        <dt>Assentos</dt>
        <dd>{consulta.data.assentos.join(', ')}</dd>
        <dt>Total</dt>
        <dd>{preco(consulta.data.total_centavos)}</dd>
      </dl>
    </section>
  )
}

function Situacao({ pedido, esgotou, aoVerificar }: { pedido: Pedido; esgotou: boolean; aoVerificar: () => void }) {
  const mapa = `/sessoes/${String(pedido.sessao_id)}`
  switch (pedido.status) {
    case 'aguardando_pagamento':
      return esgotou ? (
        <>
          <h1 id="titulo-pedido" className={styles.titulo}>
            Estamos confirmando seu pagamento
          </h1>
          <p>Assim que o banco aprovar, você receberá o e-mail com os ingressos.</p>
          <button type="button" className={styles.secundario} onClick={aoVerificar}>
            Verificar de novo
          </button>
        </>
      ) : (
        <h1 id="titulo-pedido" className={styles.titulo}>
          Confirmando seu pagamento…
        </h1>
      )
    case 'pago':
      return (
        <>
          <h1 id="titulo-pedido" className={styles.titulo}>
            Pagamento confirmado
          </h1>
          <p>Enviamos os ingressos com QR para o seu e-mail. Guarde o código do pedido.</p>
        </>
      )
    case 'expirado':
      return (
        <>
          <h1 id="titulo-pedido" className={styles.titulo}>
            O prazo do pedido acabou
          </h1>
          <p>
            Se algum valor chegou a ser cobrado, ele será estornado automaticamente. <Link to={mapa}>Escolher assentos de novo</Link>
          </p>
        </>
      )
    case 'falhou':
      return (
        <>
          <h1 id="titulo-pedido" className={styles.titulo}>
            O pagamento foi recusado
          </h1>
          <p>
            Nada foi cobrado. <Link to={mapa}>Escolher assentos de novo</Link>
          </p>
        </>
      )
    case 'estorno_pendente':
    case 'estornado':
      return (
        <>
          <h1 id="titulo-pedido" className={styles.titulo}>
            {pedido.status === 'estornado' ? 'Pagamento estornado' : 'Estorno em andamento'}
          </h1>
          <p>O pagamento chegou depois do prazo e os assentos não puderam ser mantidos. O valor volta ao seu meio de pagamento.</p>
        </>
      )
  }
}
