# PRD 0025 — Estorno + reconciliação no worker (E6, T4)

- **Task:** docs/tasks/0025-estorno-reconciliacao.md
- **Branch:** feature/0025-estorno-reconciliacao
- **Data:** 2026-09-30
- **Status:** concluído

## Objetivo

Fechar a saga fora da TX (ADR 0010). A saga não tem fila: o que falha entre o gateway e o banco é recuperado por tarefas periódicas no worker, sempre pelo mesmo CAS. Todo pagamento que não virou venda é estornado automaticamente. Todo pedido vencido é resolvido: cobrado, se o webhook se perdeu, ou expirado, com a cobrança cancelada e os assentos devolvidos. Fontes: `docs/refinamentos/E6-saga-checkout.md` §T4, ADR 0010, pendências das auditorias 0023/0024.

## Escopo

Porta `Gateway` estendida (consultar, cancelar, estornar) com os adapters Stripe e fake; `Reconciliar`; `ExecutarEstornos`; cancelamento da cobrança na expiração lazy; tarefas no worker; migration 013 (índices parciais).

## Fora de escopo

- Métricas `saga_*`, `pedidos_presos`, regras de alerta e consumidor stub (0026). Nesta task, falha repetida de estorno é log de erro a partir da 5ª tentativa.
- Replay da DLQ, prefetch e health do worker (0027).
- Cancelamento e estorno pedidos pelo operador (E9).

## Requisitos funcionais

- RF01 — `Reconciliar` (worker, a cada 1 min, até 20 pedidos por rodada, mais antigos antes): pedidos `aguardando_pagamento` com `expira_em <= agora`.
- RF02 — Para cada vencido com cobrança, **consultar** no gateway:
  - **aprovada** (webhook perdido) → `AplicarPagamento`, o **mesmo pivô** do webhook. Os holds têm 2 min de margem; depois disso vira estorno `emissao`.
  - **pendente** → **cancelar** a cobrança e então expirar. Se o gateway recusar o cancelamento (acabou de ser paga), o pedido fica para a próxima rodada.
  - **cancelada** → expirar.
  - Pedido sem cobrança → expirar.
  - Erro no gateway → adiado para a próxima rodada.
- RF03 — Expirar = CAS `aguardando_pagamento → expirado` + holds devolvidos, na mesma TX. Se outro caminho chegou antes, não faz nada.
- RF04 — `ExecutarEstornos` (worker, a cada 1 min, até 10 por rodada): pedidos `estorno_pendente`.
  - A chamada ao gateway fica **fora de TX**, com `Idempotency-Key` `estorno-{pedido}`: repetir nunca estorna duas vezes.
  - Sucesso → CAS `estorno_pendente → estornado` + holds ainda presos devolvidos (caso de divergência com o pedido aguardando) + funil `estornado`.
- RF05 — Falha no estorno → `tentativas_estorno++` e backoff exponencial por tentativa (1, 2, 4… min, teto de 1 h). A partir da 5ª tentativa, cada falha é log de **erro**. O pedido permanece `estorno_pendente`, observável.
- RF06 — Estorno aceito pelo gateway mas CAS não gravado: a próxima rodada repete com a mesma chave (o gateway devolve o mesmo estorno) e fecha. CAS perdido para outra instância → sucesso.
- RF07 — Expiração lazy na criação (0023): depois do commit, cancela a cobrança do pedido vencido, em melhor esforço (pendência da auditoria 0023).
- RF08 — Adapter Stripe:
  - `ConsultarCobranca` = `PaymentIntents.Retrieve`: `succeeded` → aprovada, `canceled` → cancelada, demais → pendente.
  - `CancelarCobranca` = `PaymentIntents.Cancel`.
  - `Estornar` = `Refunds.Create(payment_intent)` com `Idempotency-Key`.
  - Todas passam pelo breaker, pelo prazo e pelas métricas `gateway_*` (helper `chamar`).
- RF09 — Fake: estado por cobrança (`Aprovar`), cancelamento recusado se aprovada, estornos registrados por chave, `FalharEstornos(n)`.
- RF10 — As tarefas rodam em `-mode=worker|all`, no `WaitGroup` do shutdown gracioso. O worker passa a montar reserva e pedido (sem registrar as rotas deles).

## Requisitos não funcionais

- RNF01 — Nenhuma chamada ao gateway dentro de TX. Todas as transições são por CAS.
- RNF02 — Índices parciais `(expira_em) WHERE aguardando_pagamento` e `(atualizado_em) WHERE estorno_pendente`: as varreduras não crescem com o histórico.
- RNF03 — Logs só com `pedido_id`, tentativas e erro descrito, sem payload do gateway.
- RNF04 — Testes com gatilho explícito (`Reconciliar`/`ExecutarEstornos`), relógio injetado e fake programável, sem ticker nem sleep.

## Regras de negócio

- RN01 — Nenhum pagamento é "esquecido": termina `pago` (com ingressos) ou `estornado`.
- RN02 — Nenhum pedido vencido segura assentos além de uma rodada da reconciliação.

## Critérios de aceite

- [x] CA01 — Estorno pendente (divergência, holds presos) → estornado, chave `estorno-{pedido}`, trilha `estorno_pendente>estornado`, assentos devolvidos, 2ª rodada sem chamada, funil `estornado`.
- [x] CA02 — Estorno falha → tentativa 1, ainda pendente; antes do backoff não chama o gateway; depois conclui com a **mesma** chave.
- [x] CA03 — Vencido pendente → cobrança cancelada, expirado, assentos devolvidos; pedido no prazo intocado.
- [x] CA04 — Vencido pago sem webhook → pago com ingressos; webhook atrasado depois → sem efeito.
- [x] CA05 — Pago tarde demais com assento levado → estorno `emissao` → estornado.
- [x] CA06 — Reconciliação × webhook do mesmo pagamento em paralelo (5 rodadas) → efeito único.
- [x] CA07 — Expiração lazy cancela a cobrança do vencido.
- [x] CA08 — Adapter Stripe: rotas, métodos, mapeamento de status, cancelamento recusado sem contar no breaker, estorno com chave e `payment_intent`. Fake: estados e falhas programadas. Backoff: 1, 2, 4 min… até 1 h.
- [x] CA09 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Adapter contra servidor falso; fake; backoff | unit | CA08 |
| Tarefas com PG real, reserva real, fake do gateway, relógio injetado | integração | CA01–CA07 |

## Plano de implementação

1. Porta + fake + adapter Stripe (helper `chamar`).
2. Migration 013 + queries.
3. `tarefas.go` (reconciliar, estornar, backoff) + cancelamento na expiração lazy.
4. Wiring no worker.
5. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `golang-concurrency`, `golang-testing`.

## Arquivos que serão criados

- `migrations/013_pedidos_tarefas.{up,down}.sql`
- `internal/pedido/tarefas.go`, `internal/pedido/tarefas_test.go`
- `docs/tasks/0025-estorno-reconciliacao.md`, `docs/prd/0025-estorno-reconciliacao.md`

## Arquivos que serão modificados

- `internal/pedido/pagamento/{gateway.go, fake.go, stripe.go, stripe_test.go}`
- `internal/pedido/{queries.sql, repositorio.go, service.go, pivo.go, handler_test.go}`, `internal/pedido/db/queries.sql.go` (gerado)
- `cmd/morfeu/main.go`, `sqlc.yaml`
- `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total: 23 (+ `docs/ambiente-dev.md`, nota operacional pedida pela auditoria).

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- O worker passa a chamar o gateway (precisa da mesma config de pagamento da API).
- Tarefa periódica nova no worker, que para no shutdown gracioso.

## Riscos

- Estorno feito sem registro (queda entre o gateway e o banco) → fechado na rodada seguinte pela mesma chave.
- Muitos pendentes → lote limitado por rodada (Stripe test ~25 req/s).

## Estratégia de rollback

Reverter o merge (a migration 013 só cria índices). Sem as tarefas, os pedidos voltam a depender só do webhook e da expiração lazy.
