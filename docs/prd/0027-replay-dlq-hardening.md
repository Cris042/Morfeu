# PRD 0027 — Replay da DLQ + hardening do worker (E6, T6)

- **Task:** docs/tasks/0027-replay-dlq-hardening.md
- **Branch:** feature/0027-replay-dlq-hardening
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Fechar o E6. Esta task entrega:

- O caminho de volta das mensagens mortas: replay da DLQ pelo shell, idempotente. O ADR 0007 adiou o replay para o E6.
- Um worker endurecido:
  - panic de handler não derruba o processo;
  - toda DLQ é medida;
  - a outbox publicada não cresce sem fim;
  - o shutdown respeita a entrega em curso;
  - nenhum pagamento de pedido expirado fica sem estorno.

Fontes: `docs/refinamentos/E6-saga-checkout.md` §T6, ADR 0007/0010, pendências das auditorias 0025 (NB2) e 0026 (DLQ nova sem medição).

## Escopo

Replay, recuperação de panic, gauge de DLQ multi-fila, limpeza da outbox, `stop_grace_period`, varredura de cobranças abertas de expirados (migration 014) e documentação operacional.

## Fora de escopo

- Endpoint HTTP de replay: o refinamento decidiu só CLI, sem superfície nova (§6.6).
- Limpeza de `processed_messages`: é a janela do dedup de um replay tardio. Apagá-la cedo deixaria um replay reprocessar efeitos. Vai para o E11, junto com a de pedidos e holds.
- Prefetch: permanece **1**, decisão registrada na 0005. Com nack e requeue, é o que mantém a semântica do `x-delivery-limit`. O "prefetch 10" sugerido no refinamento foi descartado por esse motivo.

## Requisitos funcionais

- RF01 — Subcomando `replay-dlq -fila <dlq> [-limite N=10] [-dry-run]`. Segue o padrão dos subcomandos `criar-filme` e `seed-operador`, em vez do `-mode=replay` do refinamento: é uma operação pontual, não um modo de processo. Só pelo shell (`docker compose exec app /app replay-dlq …`).
- RF02 — **Lista fechada** de DLQs (`broker.FilasReplay`), cada uma com a routing key da fila de origem: `catalogo.filme_criado.dlq → catalogo.filme_criado` e `notificacao.pedido_confirmado.dlq → pedido.confirmado`. Fora da lista → erro, sem nada publicado. O operador nunca informa routing key.
- RF03 — Replay, até o limite: `basic.get` sem ack, depois republicação na `morfeu.events` com **publisher confirm**, depois **ack na DLQ**.
  - Preserva `message_id` (chave do dedup), `type`, `timestamp`, `content_type` e os headers de negócio (`aggregate_id`, `occurred_at`, `traceparent`).
  - Não copia `x-death`: a contagem de entregas recomeça.
  - Queda entre o publish e o ack duplica, e o dedup do consumidor absorve. Nunca perde.
- RF04 — `-dry-run` lê e lista sem publicar. Ao fechar o canal, as mensagens voltam à DLQ.
- RF05 — Saída e log: fila, dry-run, lidas, republicadas e `message_id`s. Nunca payload.
- RF06 — **Recuperação de panic** no processamento de cada entrega: vira erro permanente → DLQ, e o consumidor segue vivo.
- RF07 — `morfeu_dlq_mensagens{fila}` para **todas** as DLQs de `FilasReplay`. O alerta existente (`delta` > 0 em 15 min, sem filtro de fila) passa a cobrir a DLQ de notificação.
- RF08 — Limpeza da outbox (worker, a cada 1 h): apaga em lotes de 1000 os eventos **publicados** há mais de 7 dias. Pendentes nunca.
- RF09 — `stop_grace_period: 45s` no serviço `app` do compose. A entrega em curso termina em até 30 s (`processamentoTimeout`); o padrão de 10 s a cortaria.
- RF10 — **Cobranças abertas de expirados** (auditoria 0025, NB2):
  - Nova coluna `pedidos.cobranca_encerrada`. Fica `true` quando a cobrança do pedido que não virou venda foi cancelada no gateway ou já estava cancelada. Isso vale para a expiração lazy e para a reconciliação.
  - A reconciliação varre `expirado` com cobrança aberta (até 20 por rodada) e age conforme a cobrança:
    - **aprovada** (o cliente pagou, o cancelamento foi recusado e o webhook se perdeu) → mesmo pivô → `estorno_pendente`/`tardio` → estorno automático;
    - **pendente** → cancela;
    - **cancelada** → encerra.

## Requisitos não funcionais

- RNF01 — AMQP só em `internal/broker` (depguard). O replay é método do `broker.Client`.
- RNF02 — Testes com RabbitMQ e PG reais (harness do pacote `outbox`), sem sleep fixo (`pollUntil`).
- RNF03 — Índice parcial para a varredura de cobranças abertas.

## Regras de negócio

- RN01 — Replay nunca cria efeito duplicado: o `message_id` preservado garante o dedup.
- RN02 — Todo pagamento termina em venda ou estorno, inclusive o que chega em pedido expirado sem webhook.

## Critérios de aceite

- [ ] CA01 — Dry-run: 3 lidas, 0 republicadas; DLQ continua com 3; fila de origem vazia.
- [ ] CA02 — Replay com limite 2: 2 republicadas na fila de origem com o **mesmo** `message_id` e `aggregate_id`, sem `x-death`; 1 fica na DLQ.
- [ ] CA03 — Fila fora da lista → `ErrFilaNaoPermitida`.
- [ ] CA04 — Handler em panic → entrega na DLQ; a mensagem seguinte é processada.
- [ ] CA05 — Limpeza: publicados há 10 dias saem; publicado há 1 dia e pendente ficam.
- [ ] CA06 — Pago com o pedido expirado e o cancelamento recusado → varredura → `estorno_pendente`/`tardio` → estornado. O abandonado com cobrança cancelada não é varrido; 2ª rodada sem trabalho.
- [ ] CA07 — Gauge de DLQ com uma série por fila.
- [ ] CA08 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Replay, panic, limpeza (RabbitMQ + PG reais) | integração (`outbox`) | CA01–CA05 |
| Cobranças abertas (PG real, fake do gateway) | integração (`pedido`) | CA06 |
| Gauge multi-fila | unit (`telemetria`) | CA07 |

## Plano de implementação

1. `broker/replay.go` + panic recovery no consumidor.
2. Subcomando no `main`; gauge multi-fila; limpeza da outbox no worker; compose.
3. Migration 014 + varredura de cobranças abertas + encerramento na expiração.
4. Testes; docs operacionais; lint; `-race`; gate.

**Skills de apoio (§4.4):** `golang-concurrency`, `golang-testing`.

## Arquivos que serão criados

- `internal/broker/replay.go`, `internal/outbox/replay_integration_test.go`
- `migrations/014_pedidos_cobranca_encerrada.{up,down}.sql`
- `docs/tasks/0027-replay-dlq-hardening.md`, `docs/prd/0027-replay-dlq-hardening.md`

## Arquivos que serão modificados

- `internal/broker/consumer.go`, `internal/outbox/{outbox.go, queries.sql, telemetria_integration_test.go}`, `internal/outbox/db/queries.sql.go` (gerado)
- `internal/telemetria/{mensageria.go, telemetria_test.go}`
- `internal/pedido/{queries.sql, service.go, tarefas.go, handler_test.go, tarefas_test.go}`, `internal/pedido/db/{queries.sql.go, models.go}` (gerados)
- `cmd/morfeu/main.go`, `docker-compose.yml`, `sqlc.yaml`, `docs/ambiente-dev.md`
- `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total: 28.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- Worker: +2 rotinas periódicas (limpeza da outbox; varredura dentro da reconciliação).
- Nova coluna em `pedidos` com default. O `ALTER` é instantâneo no PG 16.

## Riscos

- Replay de mensagem que continua falhando → volta à DLQ após 3 entregas. Limite por execução e dry-run antes.

## Estratégia de rollback

Reverter o merge e aplicar `014_pedidos_cobranca_encerrada.down.sql`. O replay e a limpeza são aditivos.
