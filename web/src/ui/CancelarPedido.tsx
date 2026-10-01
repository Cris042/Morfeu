import { useEffect, useRef, useState } from 'react'

import { ErroApi } from '../api/client'
import styles from './CancelarPedido.module.css'

function mensagemDe(erro: unknown): string {
  if (erro instanceof ErroApi) {
    if (erro.codigo === 'fora_da_janela') {
      return 'O cancelamento só é possível até 2 horas antes da sessão.'
    }
    if (erro.codigo === 'nao_cancelavel') {
      return 'Este pedido não pode mais ser cancelado.'
    }
    if (erro.status === 429) {
      return 'Muitas tentativas seguidas. Espere um minuto e tente de novo.'
    }
  }
  return 'Não conseguimos cancelar agora. Tente de novo em instantes.'
}

/**
 * Cancelamento pelo cliente (PRD 0036/0038): pede confirmação antes; a janela
 * de 2h é do servidor — o botão só aparece quando a API diz "cancelavel".
 */
export function CancelarPedido({ aoCancelar }: { aoCancelar: () => Promise<unknown> }) {
  const [confirmando, setConfirmando] = useState(false)
  const [enviando, setEnviando] = useState(false)
  const [erro, setErro] = useState('')
  // Teclado: ao abrir a confirmação o foco vai para "Manter pedido" (a opção
  // segura); ao desistir, volta para o botão (auditoria 0038).
  const manter = useRef<HTMLButtonElement>(null)
  const abrir = useRef<HTMLButtonElement>(null)
  const jaAbriu = useRef(false)
  useEffect(() => {
    if (confirmando) {
      jaAbriu.current = true
      manter.current?.focus()
    } else if (jaAbriu.current) {
      abrir.current?.focus()
    }
  }, [confirmando])

  async function cancelar() {
    setEnviando(true)
    setErro('')
    try {
      await aoCancelar()
    } catch (e) {
      setErro(mensagemDe(e))
    } finally {
      setEnviando(false)
    }
  }

  return (
    <div className={styles.cancelar}>
      {confirmando ? (
        <div role="group" aria-labelledby="pergunta-cancelar" className={styles.confirmacao}>
          <p id="pergunta-cancelar">
            Cancelar este pedido? Os ingressos deixam de valer na hora e o valor volta ao meio de pagamento.
          </p>
          <div className={styles.acoes}>
            <button type="button" className={styles.perigo} disabled={enviando} onClick={() => void cancelar()}>
              {enviando ? 'Cancelando…' : 'Sim, cancelar'}
            </button>
            <button ref={manter} type="button" className={styles.secundario} disabled={enviando} onClick={() => { setConfirmando(false) }}>
              Manter pedido
            </button>
          </div>
        </div>
      ) : (
        <button ref={abrir} type="button" className={styles.secundario} onClick={() => { setConfirmando(true) }}>
          Cancelar pedido
        </button>
      )}
      <p className={styles.erro} role="alert">
        {erro}
      </p>
    </div>
  )
}
