# PRD 0044 — Limpeza operacional (E11, T3)

- **Task:** docs/tasks/0044-limpeza-operacional.md
- **Branch:** feature/0044-limpeza-operacional
- **Data:** 2026-10-01
- **Status:** concluído

## Objetivo

Fechar a pendência herdada do E6 (`state.md`, PRDs 0026/0027): limpar `processed_messages` e holds terminais, mantendo pedidos (decisão do usuário no refinamento E11) e sem abrir brecha no dedup do replay. Fonte: `docs/refinamentos/E11-hardening-backup.md` §T3.

## Escopo

Duas limpezas em lotes no worker, guarda de idade no replay da DLQ, migration de índices, métricas, alerta e runbook.

## Fora de escopo

- Apagar pedidos (e `pedido_eventos`/`ingressos`): **mantidos** — decisão do usuário.
- Outbox publicada: já limpa a cada hora (PRD 0027, 7 dias).
- Trilha de auditoria: purga de 12 meses (PRD 0037).

## Requisitos funcionais

- RF01 — **Janela do dedup = 30 dias** (`outbox.JanelaDedup`). `outbox.LimparProcessadas` apaga `processed_messages` com `processed_at` anterior à janela, em lotes de 1000 (DELETE curto por lote, padrão da 0027/0037).
- RF02 — **Guarda no replay** (`replay-dlq`): mensagem cujo `Timestamp` (instante do evento) é anterior a `agora − JanelaDedup` **não é republicada** — fica na DLQ e é contada em `retidas` no resultado/log, com orientação no runbook (verificar o efeito à mão antes de reenviar). Justificativa: o registro do dedup nasce no consumo (≥ instante do evento); se o evento é mais novo que a janela, o registro ainda existe.
- RF03 — `reserva.LimparHoldsTerminais` apaga holds com `status IN ('liberado','expirado')` e `atualizado_em` anterior a 7 dias, em lotes de 1000. `ativo` e `convertido` (assento vendido, ocupa o índice da trava — ADR 0008/0010) **nunca** são apagados. Sem consultar `pedidos` (ADR 0003): o pivô só converte holds `ativo` do pedido `aguardando_pagamento`, que nunca dura 7 dias; webhook tardio cai no estorno `tardio` sem tocar holds.
- RF04 — Migration 019: índice em `processed_messages (processed_at)` e índice parcial `holds (atualizado_em) WHERE status IN ('liberado','expirado')`.
- RF05 — Worker (`-mode=worker|all`): uma rotina diária roda as duas limpezas na partida e a cada 24 h; métricas `limpeza_removidos_total{alvo}` e `limpeza_ultima_execucao_timestamp{alvo}` (`alvo` ∈ `processed_messages`, `holds` — cardinalidade fixa); erro de uma não impede a outra.
- RF06 — Alerta `morfeu-limpeza-parada`: alguma limpeza sem execução bem-sucedida há > 48 h (sem série = OK, como a purga); regras passam a 19, conferidas por uid no teste da stack; contrato reconhece o prefixo `limpeza_`.

## Requisitos não funcionais

- RNF01 — Nenhum lock longo: DELETEs por lote com `LIMIT` sobre o índice; a limpeza usa o pool do app (role `morfeu_app`), nunca o da purga da trilha.
- RNF02 — Logs só com contagens.

## Critérios de aceite

- [x] CA01 — `processed_messages`: linha 1 s antes da janela some, exatamente na borda/1 s depois fica; lotes (mais de 1 lote apagado por inteiro); idempotente (2ª execução apaga 0).
- [x] CA02 — Holds: `liberado`/`expirado` com 7 dias + 1 s somem; com 7 dias − 1 s ficam; `ativo` e `convertido` antigos ficam; idempotente.
- [x] CA03 — Concorrência: limpeza rodando junto com o sweeper e com uma reserva nova não perde hold vivo nem falha (teste com goroutines).
- [x] CA04 — Replay: mensagem com evento mais velho que a janela fica na DLQ (`retidas` = 1, nada republicado); mais nova é republicada.
- [x] CA05 — Alerta `morfeu-limpeza-parada` provisionado (19 regras por uid); contrato verde.
- [x] CA06 — Runbook (`docs/observabilidade.md`): alerta novo, retenções e o que fazer com mensagens retidas no replay; pendência do E6 baixada do `state.md`.
- [x] CA07 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Bordas, lote e idempotência de `processed_messages` | integração (`internal/outbox`) | CA01 |
| Bordas por status/idade e concorrência dos holds | integração (`internal/reserva`) | CA02, CA03 |
| Guarda de idade no replay | integração (`internal/outbox/replay_integration_test.go`, RabbitMQ real) | CA04 |
| Alerta e contrato | `test/observabilidade` | CA05 |

## Plano de implementação

1. Migration 019; 2. queries + sqlc; 3. funções de limpeza; 4. guarda do replay; 5. rotina + métricas no worker; 6. alerta + testes; 7. runbook.

**Skills de apoio (§4.4):** `golang-database`, `golang-testing`.

## Arquivos que serão criados

- `migrations/019_limpeza_indices.{up,down}.sql`, `internal/outbox/limpeza_integration_test.go`, `internal/reserva/limpeza.go`, `internal/reserva/limpeza_integration_test.go`
- `docs/tasks/0044-limpeza-operacional.md`, `docs/prd/0044-limpeza-operacional.md`

## Arquivos que serão modificados

- `internal/outbox/queries.sql`, `internal/outbox/db/queries.sql.go`, `internal/outbox/outbox.go`, `internal/outbox/replay_integration_test.go`
- `internal/broker/replay.go`, `internal/reserva/queries.sql`, `internal/reserva/db/queries.sql.go`, `cmd/morfeu/main.go`
- `configs/grafana/provisioning/alerting/alertas.yml`, `test/observabilidade/stack_integration_test.go`, `test/observabilidade/contrato_test.go`
- `docs/observabilidade.md`, `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total estimado: 23.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- Replay de mensagem com mais de 30 dias passa a exigir decisão manual (antes reprocessaria efeitos sem dedup quando a limpeza existisse).
- Índices novos em tabelas pequenas (criação rápida; sem `CONCURRENTLY` — golang-migrate roda em TX).

## Riscos

- Apagar hold que ainda importa → só terminais com 7 dias + testes de borda e de status.

## Desvios registrados na implementação

- `Reprocessar` recebe o instante de corte (`outbox` importa `broker`; o inverso seria ciclo); o `main` passa `agora − JanelaDedup`.
- Mensagem sem `Timestamp` é **retida** (opção segura).
- Retidas não consomem o `-limite` (mensagens velhas na frente da DLQ não bloqueiam as novas).
- Helper `morta()` dos testes de replay passa a gravar `Timestamp` (os antigos virariam retidas); `mortaEm()` novo.
- Logs dizem "limpeza diária" (o `misspell` do lint acusa "operacional"); nome do alerta e do runbook mantidos.
- Os `TestMain` de `outbox`/`reserva` não aplicam a 019 (cada um tem metade das tabelas); a 019 foi validada em PG efêmero (up → down → up, `EXPLAIN` usa o índice parcial) e pelo job `migrations` do CI.
- O gauge também é gravado na partida do worker (achado da auditoria): um alvo que nunca tem sucesso dispara o alerta 48 h depois do boot, em vez de ficar sem série.

## Estratégia de rollback

Reverter o merge; a 019 tem `down` (remove os índices). Linhas apagadas não voltam — são operacionais (dedup vencido e holds terminais), sem valor de negócio.
