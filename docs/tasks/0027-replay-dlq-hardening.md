# Task 0027 — Replay da DLQ + hardening do worker (E6, T6)

- **Data:** 2026-09-30
- **Status:** em andamento
- **Branch:** `feature/0027-replay-dlq-hardening` (da main `cd139d7`)
- **PRD:** docs/prd/0027-replay-dlq-hardening.md
- **Item do roadmap:** E6 — Saga do checkout (6ª e última; fecha o E6). Refinamento: `docs/refinamentos/E6-saga-checkout.md` §T6; ADR 0007 (replay adiado ao E6) e 0010.

## Objetivo

Dar ao operador o caminho de volta das mensagens mortas (replay da DLQ pelo shell, idempotente) e endurecer o worker: panic de handler não derruba o processo, toda DLQ é medida, a outbox não cresce sem fim, o shutdown respeita a entrega em curso e nenhum pagamento de pedido expirado fica sem estorno.

## Escopo

Subcomando `replay-dlq` (lista fechada de DLQs, `-limite`, `-dry-run`, `message_id` preservado); recuperação de panic → DLQ; gauge `morfeu_dlq_mensagens{fila}` para todas as DLQs; limpeza da outbox publicada; `stop_grace_period`; varredura de expirados com cobrança aberta (auditoria 0025).

## Fora de escopo

Endpoint HTTP de replay (decisão do refinamento: só CLI); limpeza de `processed_messages`, pedidos e holds (E11); dashboards (E10).

## Arquivos esperados

28 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [ ] Dry-run lista sem publicar; replay republica com o mesmo `message_id` até o limite; fila fora da lista recusada.
- [ ] Panic no handler → DLQ, consumidor segue vivo.
- [ ] Outbox publicada > 7 dias apagada; pendente nunca.
- [ ] Expirado com cobrança paga (webhook perdido + cancelamento recusado) → estorno.

## Riscos

- Replay mal usado republica mensagens que voltam a falhar → limite por execução + `x-delivery-limit` (sem loop) + dry-run.

## Estimativa de impacto

Médio: worker ganha duas rotinas periódicas; nenhuma rota nova.
