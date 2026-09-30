# Task 0033 — SPA: checkout e pagamento (E8, T3)

- **Data:** 2026-09-30
- **Status:** concluída (auditoria APROVADA em 2026-09-30)
- **Branch:** `feature/0033-spa-checkout` (da main `f539bdf`)
- **PRD:** docs/prd/0033-spa-checkout.md
- **Item do roadmap:** E8 — SPA checkout + convidado/conta (3ª de 5). Refinamento: `docs/refinamentos/E8-spa-checkout.md` §T3.

## Objetivo

Levar o cliente de "Seus assentos" ao pagamento e ao resultado: e-mail (ou o da conta) → pedido → Payment Element (ou "Pagar (teste)" no modo fake) → acompanhamento até o webhook decidir.

## Escopo

Tela de checkout, meios de pagamento (Stripe e teste, um por build), página de acompanhamento com polling, retomada de pedido pendente, CSP do Stripe no Caddy, `allow_redirects=never` no PaymentIntent, gates de bundle no CI, deps do Stripe.

## Fora de escopo

Consulta de convidado e página do ingresso (0034/0035); E2E do M4 e axe (0035).

## Arquivos esperados

30 (lista no PRD).

## Dependências esperadas

`@stripe/stripe-js` 9.17.0 e `@stripe/react-stripe-js` 6.12.0 (runtime, só no chunk do checkout).

## Critérios de aceite

- [x] Pedido criado com os assentos da sessão; e-mail da conta pré-preenchido; Bearer quando logado.
- [x] 409 pendente → retomada; holds inválidos/404 → volta ao mapa; 400/429/503/rede com mensagens próprias.
- [x] Payment Element sem redirecionamento; recusa mostrada; fake chama a rota de teste.
- [x] Polling com backoff, pausa sem foco, parada no terminal e teto com aviso do e-mail.
- [x] Bundle de produção sem pagamento de teste e sem segredos; CSP libera só o Stripe necessário.

## Riscos

- Stripe.js bloqueado pela CSP em produção → diretivas conferidas contra a doc; validação real no E0c-CD (Caddy).
- Webhook atrasado → teto do polling com a mensagem de que o e-mail chegará.

## Estimativa de impacto

Médio: telas novas + 1 parâmetro do PaymentIntent.
