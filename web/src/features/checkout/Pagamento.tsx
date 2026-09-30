import { lazy, Suspense } from 'react'

import { Carregando } from '../../ui/Estado'
import type { PropsPagamento } from './tipos'

// Um único meio por build (PRD 0033): com VITE_PAGAMENTO_MODO=fake (dev/CI)
// o botão de teste; senão o Payment Element. A constante é resolvida no
// build, então o chunk do outro modo nem é gerado — o bundle de produção não
// contém a rota de teste, e o de teste não carrega o Stripe.js.
const modoFake = import.meta.env.VITE_PAGAMENTO_MODO === 'fake'

const Meio = modoFake
  ? lazy(() => import('./PagarTeste').then((m) => ({ default: m.PagarTeste })))
  : lazy(() => import('./PagamentoStripe').then((m) => ({ default: m.PagamentoStripe })))

export function Pagamento(props: PropsPagamento) {
  return (
    <Suspense fallback={<Carregando texto="Abrindo o pagamento…" />}>
      <Meio {...props} />
    </Suspense>
  )
}
