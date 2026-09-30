import { useState } from 'react'

import { pagarParaTeste } from '../../api/checkout'
import styles from './Checkout.module.css'
import type { PropsPagamento } from './tipos'

/**
 * Pagamento de teste (dev/CI, gateway fake): a API dispara o webhook assinado
 * pelo caminho real (PRD 0031). Nunca entra no bundle de produção.
 */
export function PagarTeste({ pedidoId, aoConcluir }: PropsPagamento) {
  const [erro, setErro] = useState('')
  const [enviando, setEnviando] = useState(false)
  return (
    <div className={styles.meio}>
      <p className={styles.nota}>Ambiente de teste: nenhuma cobrança real.</p>
      <p className={styles.erro} role="alert">
        {erro}
      </p>
      <button
        type="button"
        className={styles.primario}
        disabled={enviando}
        onClick={() => {
          setEnviando(true)
          setErro('')
          pagarParaTeste(pedidoId)
            .then(aoConcluir)
            .catch(() => {
              setErro('O pagamento de teste falhou. Tente de novo.')
              setEnviando(false)
            })
        }}
      >
        {enviando ? 'Pagando…' : 'Pagar (teste)'}
      </button>
    </div>
  )
}
