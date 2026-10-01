import { Link, useParams } from 'react-router'

import { ErroApi } from '../../api/client'
import { caminhoDoQR, useIngresso } from '../../api/consulta'
import { Carregando, Falha } from '../../ui/Estado'
import { dia, hora } from '../../ui/formato'
import styles from './Consulta.module.css'

/**
 * Página do ingresso (link do e-mail — PRD 0035): dados da sessão + QR. Sem
 * scripts de terceiros; o QR vem da API com a mesma validação do link.
 */
export function Ingresso() {
  const { ref = '' } = useParams()
  const consulta = useIngresso(ref)

  if (consulta.isPending) {
    return <Carregando texto="Conferindo o ingresso…" />
  }
  if (consulta.isError) {
    const status = consulta.error instanceof ErroApi ? consulta.error.status : 0
    if (status === 404 || status === 410) {
      return (
        <section className={styles.consulta}>
          <h1 className={styles.titulo}>{status === 410 ? 'Este ingresso não vale mais' : 'Link de ingresso inválido'}</h1>
          <p>
            {status === 410
              ? 'A sessão já passou ou o ingresso foi cancelado.'
              : 'Confira se o link foi copiado inteiro do e-mail.'}{' '}
            <Link to="/consulta">Consultar pedido</Link>
          </p>
        </section>
      )
    }
    return <Falha texto="Não conseguimos abrir o ingresso." aoTentar={() => void consulta.refetch()} />
  }
  const i = consulta.data
  return (
    <section className={styles.ingresso} aria-labelledby="titulo-ingresso">
      <p className={styles.eyebrow}>Ingresso</p>
      <h1 id="titulo-ingresso" className={styles.titulo}>
        {i.filme}
      </h1>
      <dl className={styles.resumo}>
        <dt>Sessão</dt>
        <dd>
          <time dateTime={i.inicio}>
            {dia(i.inicio)} · {hora(i.inicio)}
          </time>
        </dd>
        <dt>Sala</dt>
        <dd>{i.sala}</dd>
        <dt>Assento</dt>
        <dd className={styles.codigo}>{i.assento}</dd>
      </dl>
      {i.status === 'usado' && <p className={styles.erro}>Este ingresso já foi usado na entrada.</p>}
      <img
        className={styles.qr}
        src={caminhoDoQR(ref)}
        alt={`QR do ingresso do assento ${i.assento}`}
        width={256}
        height={256}
        referrerPolicy="no-referrer"
      />
      <p className={styles.nota}>Apresente este QR na entrada da sala.</p>
    </section>
  )
}
