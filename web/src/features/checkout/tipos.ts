/** Contrato comum dos meios de pagamento (Stripe real ou teste). */
export interface PropsPagamento {
  pedidoId: string
  /** client_secret do PaymentIntent — só em memória, nunca persistido. */
  segredo: string
  aoConcluir: () => void
}
