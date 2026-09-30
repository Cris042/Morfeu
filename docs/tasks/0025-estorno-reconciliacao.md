# Task 0025 — Estorno + reconciliação no worker (E6, T4)

- **Data:** 2026-09-30
- **Status:** concluída (auditoria APROVADA em 2026-09-30)
- **Branch:** `feature/0025-estorno-reconciliacao` (da main `8cf6f6e`)
- **PRD:** docs/prd/0025-estorno-reconciliacao.md
- **Item do roadmap:** E6 — Saga do checkout (4ª de 6). Refinamento: `docs/refinamentos/E6-saga-checkout.md` §T4; ADR 0010.

## Objetivo

Fechar as pontas da saga fora da TX: todo pagamento que não virou venda é estornado automaticamente, e todo pedido vencido é resolvido — cobrado (webhook perdido) ou expirado com a cobrança cancelada e os assentos devolvidos.

## Escopo

Porta `pagamento.Gateway` com consultar/cancelar/estornar (Stripe + fake); job de estorno com chave `estorno-{pedido}` e backoff; reconciliador de pedidos vencidos usando o mesmo pivô do webhook; cancelamento da cobrança na expiração lazy; tarefas no worker (shutdown gracioso); índices parciais das varreduras.

## Fora de escopo

Métricas da saga e alertas versionados (0026); replay da DLQ e hardening do worker (0027); cancelamento pelo operador (E9).

## Arquivos esperados

22 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [x] Estorno pendente → estornado com a chave do pedido, assentos devolvidos, idempotente.
- [x] Falha no estorno → tentativa registrada, backoff, mesma chave depois.
- [x] Vencido sem pagamento → cobrança cancelada, expirado, assentos devolvidos.
- [x] Vencido pago sem webhook → pivô (pago ou estorno).
- [x] Reconciliação × webhook → efeito único.

## Riscos

- Chamadas externas fora de TX: janela entre o gateway e o CAS → sempre fechada pela próxima rodada (mesma chave, mesmo CAS).

## Estimativa de impacto

Médio; o worker ganha uma tarefa periódica.
