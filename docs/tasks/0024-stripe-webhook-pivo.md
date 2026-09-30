# Task 0024 — Stripe + webhook assinado + pivô do checkout (E6, T3)

- **Data:** 2026-09-30
- **Status:** concluída (auditoria APROVADA em 2026-09-30)
- **Branch:** `feature/0024-stripe-webhook-pivo` (da main `882d408`)
- **PRD:** docs/prd/0024-stripe-webhook-pivo.md
- **Item do roadmap:** E6 — Saga do checkout (3ª de 6). Refinamento: `docs/refinamentos/E6-saga-checkout.md` §T3; ADR 0010.

## Objetivo

Fechar o pivô da saga: o pagamento aprovado chega pelo webhook assinado do Stripe e, numa única TX, vira pedido pago, holds vendidos, ingressos e o evento `pedido.confirmado` — ou vai para estorno pendente quando não dá para vender.

## Escopo

`stripe-go` validada e registrada; adapter Stripe do `pagamento.Gateway` (PaymentIntent + idempotência + breaker próprio + métricas); verificação do webhook; `POST /webhooks/stripe`; pivô com dedup (`stripe_eventos`), cruzamento de valor, savepoint da emissão e estorno pendente com motivo; config de gateway com recusas de boot; Stripe CLI no compose; regra do gitleaks para `whsec_`.

## Fora de escopo

Execução do estorno e reconciliação (0025); consumidor do `pedido.confirmado` e métricas/alertas da saga (0026); token HMAC do ingresso (E7, que o usa no QR); CSP do Payment Element (E8).

## Arquivos esperados

30 (lista no PRD).

## Dependências esperadas

Nova: `github.com/stripe/stripe-go/v86` v86.4.2 (registrada no `lib.md` antes do uso).

## Critérios de aceite

- [x] Webhook aprovado → pago, ingressos, holds convertidos, `pedido.confirmado` só com o id.
- [x] 10 entregas simultâneas do mesmo evento → 1 efeito.
- [x] Assinatura inválida/adulterada/antiga/futura → 400 sem efeito; corpo > 64 KB recusado.
- [x] Divergência, pagamento tardio e assento perdido → `estorno_pendente` com motivo, sem venda parcial.
- [x] Corrida webhook × expiração → sempre estado válido.
- [x] Boot recusa chave live, Stripe sem chave e fake em produção.

## Riscos

- Webhook é rota pública que mexe com dinheiro → assinatura sobre o corpo bruto + cruzamento + testes negativos.

## Estimativa de impacto

Alto no módulo `pedido`; nenhum nas rotas existentes.
