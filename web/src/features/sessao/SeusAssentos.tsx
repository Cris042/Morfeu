import { useEffect } from 'react'
import { Link } from 'react-router'

import { useEstender, useLiberar } from '../../api/consultas'
import type { Hold } from '../../api/tipos'
import { contagem } from '../../ui/formato'
import styles from './PaginaSessao.module.css'

const AVISO_MS = 60_000

interface Props {
  sessaoID: number
  holds: Hold[]
  agora: number
  aoVencer: () => void
}

/**
 * Painel "Seus assentos" (PRD 0020 RF05): contagem por expira_em, extensão
 * única e liberação. A contagem é aviso — quem decide é o servidor.
 */
export function SeusAssentos({ sessaoID, holds, agora, aoVencer }: Props) {
  const estender = useEstender(sessaoID)
  const liberar = useLiberar(sessaoID)
  const restantes = holds.map((h) => ({ hold: h, ms: Date.parse(h.expira_em) - agora }))
  const algumVenceu = restantes.some((r) => r.ms <= 0)

  useEffect(() => {
    if (algumVenceu) {
      aoVencer() // reconsulta: o vencido some da lista (o servidor confirma)
    }
  }, [algumVenceu, aoVencer])

  const vivos = restantes.filter((r) => r.ms > 0)
  if (vivos.length === 0) {
    return null
  }
  const urgente = vivos.some((r) => r.ms <= AVISO_MS)

  return (
    <aside className={styles.painel} aria-labelledby="titulo-seus">
      <h2 id="titulo-seus" className={styles.subtitulo}>
        Seus assentos
      </h2>
      {urgente && (
        <p className={styles.urgente} role="status">
          Seu assento expira em menos de um minuto.
        </p>
      )}
      <ul className={styles.lista}>
        {vivos.map(({ hold, ms }) => (
          <li key={hold.id} className={styles.hold}>
            <span className={styles.codigo}>{hold.assento}</span>
            <span className={styles.relogio} aria-label={`Reservado por mais ${contagem(ms)}`}>
              <time>{contagem(ms)}</time>
            </span>
            <button
              type="button"
              className={styles.secundario}
              disabled={hold.extensoes_usadas >= 1 || estender.isPending}
              onClick={() => {
                estender.mutate(hold.id)
              }}
            >
              Mais 10 minutos
            </button>
            <button
              type="button"
              className={styles.secundario}
              disabled={liberar.isPending}
              onClick={() => {
                liberar.mutate(hold.id)
              }}
            >
              Liberar {hold.assento}
            </button>
          </li>
        ))}
      </ul>
      <Link to={`/sessoes/${String(sessaoID)}/pagamento`} className={styles.continuar}>
        Continuar para o pagamento
      </Link>
    </aside>
  )
}
