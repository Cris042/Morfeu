import { Elements, PaymentElement, useElements, useStripe } from '@stripe/react-stripe-js'
import { loadStripe } from '@stripe/stripe-js'
import type { Appearance } from '@stripe/stripe-js'
import { useState } from 'react'
import type { SubmitEvent } from 'react'

import styles from './Checkout.module.css'
import type { PropsPagamento } from './tipos'

// Stripe.js vem sempre de js.stripe.com (PCI; CSP do Caddy libera só isso) e
// só é pedido quando este chunk carrega — nunca fora do checkout.
const chave = import.meta.env.VITE_STRIPE_PK
const stripe = chave ? loadStripe(chave).catch(() => null) : undefined

// Tokens "A Sala Escura" passados ao iframe do Stripe (sem style inline aqui).
const aparencia: Appearance = {
  theme: 'night',
  variables: {
    colorPrimary: '#ff5c45',
    colorBackground: '#1e1830',
    colorText: '#f2ecdf',
    colorDanger: '#e9b95b',
    borderRadius: '3px',
  },
}

export function PagamentoStripe({ segredo, aoConcluir }: PropsPagamento) {
  if (!stripe) {
    return (
      <p className={styles.erro} role="alert">
        O pagamento com cartão não está disponível agora. Seus assentos continuam reservados até o prazo do pedido.
      </p>
    )
  }
  return (
    <Elements stripe={stripe} options={{ clientSecret: segredo, appearance: aparencia, locale: 'pt-BR' }}>
      <Formulario aoConcluir={aoConcluir} />
    </Elements>
  )
}

function Formulario({ aoConcluir }: { aoConcluir: () => void }) {
  const s = useStripe()
  const elements = useElements()
  const [erro, setErro] = useState('')
  const [enviando, setEnviando] = useState(false)

  async function pagar(ev: SubmitEvent<HTMLFormElement>) {
    ev.preventDefault()
    if (!s || !elements) {
      return
    }
    setEnviando(true)
    setErro('')
    // Sem métodos de redirecionamento (allow_redirects=never no backend): a
    // confirmação termina aqui e o client_secret nunca vai para uma URL.
    const r = await s.confirmPayment({ elements, redirect: 'if_required' })
    if (r.error) {
      setErro(r.error.message ?? 'O pagamento não foi concluído. Confira os dados e tente de novo.')
      setEnviando(false)
      return
    }
    aoConcluir()
  }

  return (
    <form className={styles.meio} onSubmit={(ev) => void pagar(ev)}>
      <PaymentElement />
      <p className={styles.erro} role="alert">
        {erro}
      </p>
      <button type="submit" className={styles.primario} disabled={!s || !elements || enviando}>
        {enviando ? 'Pagando…' : 'Pagar'}
      </button>
    </form>
  )
}
