# PRD 0022 — Reserva: holds vendidos ocupam o assento + portas transacionais do pedido (E6, T1)

- **Task:** docs/tasks/0022-reserva-portas-pedido.md
- **Branch:** feature/0022-reserva-portas-pedido
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Preparar a `reserva` para o checkout do E6. Um hold vendido (`convertido`) passa a ocupar o assento de forma definitiva. O service do `pedido` passa a prender, converter e liberar os holds do carrinho **dentro da própria transação**, por métodos que recebem a `outbox.Tx` (ADR 0010, forma aprovada de cruzar módulos numa TX). Fontes: `docs/refinamentos/E6-saga-checkout.md` §T1, ADR 0010 e ADR 0008.

## Escopo

Migration `010`, queries, métodos transacionais, 409 `hold_em_pedido`, ocupação, métrica e testes de regressão. A porta ainda não é consumida: o `pedido` e o wiring nascem na task 0023.

## Fora de escopo

Módulo `pedido`, tabelas `pedidos`/`ingressos` e o adapter no `main` (0023); Stripe/webhook (0024); cancelamento `convertido → liberado` do operador (E9); purge de holds terminais (hardening).

## Requisitos funcionais

- RF01 — **Índice:** `holds_assento_ativo` é substituído por `holds_assento_ocupado` = `UNIQUE (sessao_id, assento_codigo) WHERE status IN ('ativo','convertido')`. O `ON CONFLICT` da trava usa **o mesmo predicado**, e o `DO UPDATE` só rouba quando `holds.status = 'ativo' AND holds.expires_at <= agora`, zerando `pedido_id`. Consequência: assento vendido → 409 `assento_indisponivel` para sempre.
- RF02 — **Coluna `pedido_id uuid NULL`** nos holds: marca o hold preso a um pedido. Sem FK: `pedidos` pertence a outro módulo (ADR 0003).
- RF03 — `PrenderParaPedido(ctx, tx, dono, sessaoID, códigos, pedidoID, até)`: numa só instrução, fixa `expires_at = até` e `pedido_id` nos holds **vivos** do dono naquela sessão com aqueles códigos e `pedido_id` nulo ou igual (idempotente). Se não cobrir todos os códigos → `ErrHoldsDoPedido`, e quem chamou desfaz a TX. Não consome `extensoes_usadas`. Lote validado (`NovoLote`: 1–6, sem repetição).
- RF04 — `ConverterDoPedido(ctx, tx, pedidoID)`: `ativo → convertido` em todos os holds do pedido (sem olhar o prazo: se ainda está `ativo`, ninguém o roubou, porque o roubo zera `pedido_id`). Devolve os códigos convertidos do pedido, incluindo os já convertidos antes, o que torna a operação **idempotente**. Quem chama compara com o esperado (conflito → estorno, na 0024).
- RF05 — `LiberarDoPedido(ctx, tx, pedidoID)`: `ativo → liberado` nos holds do pedido. Devolve quantos liberou. Não toca `convertido`.
- RF06 — Hold preso (`pedido_id` não nulo): `POST /holds/{id}/estender` e `DELETE /holds/{id}` (liberar) do cliente → **409 `hold_em_pedido`**. Continua listado em `GET /holds` e conta no teto de 6.
- RF07 — Ocupação: `(status='ativo' AND expires_at > agora) OR status='convertido'`.
- RF08 — Sweeper inalterado (só `ativo` vencido). Hold preso vence em `até` e só então é varrido.
- RF09 — Métrica `reserva_holds_convertidos_total` (callback `Metricas.Convertidos`, sem labels), incrementada pelo número de holds que mudaram de fato em `ConverterDoPedido`.
- RF10 — `Dono` ganha `Hash()` e `DonoDoHash([]byte)` para o adapter do `main` (o `pedido` guarda o hash do carrinho — refinamento §T2).

## Requisitos não funcionais

- RNF01 — Só estes 3 métodos recebem `Tx` (ADR 0010, "Estratégias"). O `pedido` não importa a `reserva`: a porta é declarada no consumidor, na 0023.
- RNF02 — Migration com down. O down recria o índice antigo e remove a coluna. Fica documentado que ele falha se houver hold `convertido` ativo no mesmo assento de um `ativo`, o que não ocorre antes da 0023.
- RNF03 — Nenhum dado novo em logs além de contagens e `sessao_id`.

## Regras de negócio

- RN01 — Vendido é para sempre na trava. Só o E9 (cancelamento) devolve.
- RN02 — O hold preso vale até `até` (`expira_em` do pedido + 2 min, decidido pelo `pedido`). Depois disso volta a ser roubável como qualquer vencido, e o pagamento tardio vira estorno (ADR 0010).

## Critérios de aceite

- [ ] CA01 — Hold `convertido`: nova trava do mesmo assento → 409, inclusive com o relógio muito adiante e sob corrida de 20 donos. A linha do convertido fica intacta (mesmo `id`, dono e `pedido_id`).
- [ ] CA02 — Hold preso até T+17 min: outro dono com o relógio em T+12 min → 409; em T+17 min → rouba (201) e `pedido_id` volta a nulo.
- [ ] CA03 — `PrenderParaPedido` com assento de outro dono, vencido ou fora do conjunto → `ErrHoldsDoPedido` e nada é alterado (a TX desfaz). Com os mesmos argumentos 2× → ok.
- [ ] CA04 — `ConverterDoPedido` 2× → mesmos códigos; métrica incrementa só na 1ª. Hold do pedido roubado (após vencer) → devolve só os restantes.
- [ ] CA05 — `LiberarDoPedido` libera os presos, e o assento fica travável por outro dono.
- [ ] CA06 — Cliente estende/libera hold preso → 409 `hold_em_pedido`.
- [ ] CA07 — Ocupação lista `convertido` (mesmo vencido pelo relógio) e não lista `liberado`.
- [ ] CA08 — Sweeper com relógio adiante não altera `convertido` nem hold preso antes do prazo.
- [ ] CA09 — Suíte existente do E4 (corrida canônica, roubo, lotes, teto, sweeper, ocupação) e E2E do M3 verdes.
- [ ] CA10 — CI verde (lint, `-race`, sqlc vet, migrations).

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Portas do pedido pela API do serviço com TX real (`outbox.WithTx`), PG + Redis reais, relógio injetado | integração (`portas_integration_test`, mesmo `TestMain`) | CA01–CA05, CA07, CA08 |
| Rotas estender/liberar em hold preso | integração HTTP | CA06 |
| Suíte do E4 | integração | CA09 |

## Plano de implementação

1. Migration `010_holds_pedido` (coluna + índice) e ajuste das queries (`TravarAssento`, `OcupadosDaSessao`, `EstenderHold`/`LiberarHold` com `pedido_id IS NULL`) + queries novas; `sqlc generate`.
2. `pedido.go` na `reserva`: os 3 métodos transacionais + `ErrHoldsDoPedido`, `ErrHoldEmPedido`.
3. Handler: mapear `hold_em_pedido` → 409; serviço distingue "preso" de "não encontrado".
4. `Metricas.Convertidos` + wiring no `main`.
5. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `golang-database`, `golang-testing`.

## Arquivos que serão criados

- `migrations/010_holds_pedido.up.sql`, `migrations/010_holds_pedido.down.sql`
- `internal/reserva/pedido.go`, `internal/reserva/pedido_test.go` (integração)
- `docs/tasks/0022-reserva-portas-pedido.md`, `docs/prd/0022-reserva-portas-pedido.md`

## Arquivos que serão modificados

- `internal/reserva/queries.sql`, `internal/reserva/db/queries.sql.go`, `internal/reserva/db/models.go` (gerados), `internal/reserva/service.go`, `internal/reserva/errors.go`, `internal/reserva/handler.go`, `internal/reserva/carrinho.go`, `internal/reserva/handler_test.go` (migration nova no `TestMain`)
- `cmd/morfeu/main.go` (métrica)
- `docs/tasks/README.md`, `docs/tasks/0021-e2e-m3.md`, `docs/prd/0021-e2e-m3.md` (status), `plan.md`, `state.md`

Total: ~21.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- A invariante central da trava muda de predicado (ADR 0008 complementado pelo ADR 0010).
- Novo código de erro público `hold_em_pedido` (o SPA do E8 trata; o E5 só verá o erro se houver pedido, o que só acontece a partir do E8).

## Riscos

- Regressão da corrida/roubo → a suíte do E4 continua sendo gate.
- `ON CONFLICT` com predicado diferente do índice falharia em runtime ("no unique constraint matching") → o teste da trava cobre qualquer divergência.

## Estratégia de rollback

Reverter o merge e aplicar `010_holds_pedido.down.sql` (seguro enquanto não houver pedidos, ou seja, antes da 0023 em produção).
