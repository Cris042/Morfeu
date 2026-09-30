# Task 0022 — Reserva: holds vendidos ocupam o assento + portas transacionais do pedido (E6, T1)

- **Data:** 2026-09-30
- **Status:** em andamento
- **Branch:** `feature/0022-reserva-portas-pedido` (da main `4fbe8ec`)
- **PRD:** docs/prd/0022-reserva-portas-pedido.md
- **Item do roadmap:** E6 — Saga do checkout (1ª de 6). Refinamento: `docs/refinamentos/E6-saga-checkout.md` §T1; ADR 0010.

## Objetivo

Preparar a `reserva` para o checkout: um assento vendido (hold `convertido`) continua ocupado para sempre, e o `pedido` passa a poder prender, converter e liberar os holds do carrinho dentro da própria transação — sem que um módulo importe o outro.

## Escopo

Migration do índice único parcial (`ativo` + `convertido`) e da coluna `pedido_id` nos holds; trava rouba só `ativo` vencido; ocupação conta `convertido`; métodos transacionais `PrenderParaPedido`, `ConverterDoPedido`, `LiberarDoPedido`; hold preso não é estendido nem liberado pelo cliente (409 `hold_em_pedido`); métrica de convertidos; testes de regressão do E4.

## Fora de escopo

Módulo `pedido`, tabelas `pedidos`/`ingressos` e o wiring da porta (task 0023); Stripe (0024); cancelamento do operador (E9).

## Arquivos esperados

~17 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [ ] Hold `convertido` não é roubado nem sobrescrito por nova trava (409) e aparece na ocupação.
- [ ] Hold preso para um pedido não é roubado antes do novo prazo; o cliente não o estende nem libera (409 `hold_em_pedido`).
- [ ] `ConverterDoPedido` é idempotente; `LiberarDoPedido` devolve os assentos.
- [ ] Sweeper não toca `convertido`.
- [ ] Suíte do E4 e E2E do M3 verdes.

## Riscos

- Mudança no índice da trava (ADR 0008) → corrida canônica e roubo continuam como gate.

## Estimativa de impacto

Médio na `reserva` (índice da invariante central); nenhum em rotas públicas além do 409 novo.
