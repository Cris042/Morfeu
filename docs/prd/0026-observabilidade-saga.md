# PRD 0026 — Observabilidade da saga + consumidor de notificação + integração ponta a ponta (E6, T5)

- **Task:** docs/tasks/0026-observabilidade-saga.md
- **Branch:** feature/0026-observabilidade-saga
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Tornar a saga do checkout observável (doc.md §13, "monitoramento da saga") e provar o caminho inteiro de ponta a ponta:

- Compensações contadas por passo e pedidos presos expostos.
- Latência medida entre o pagamento confirmado e a notificação.
- Alertas versionados.
- A fila e o consumidor de `pedido.confirmado` (stub até o e-mail do E7).
- Um teste de integração da trava até a notificação processada, incluindo a notificação que falha e **nunca** desfaz a venda.

Fontes: `docs/refinamentos/E6-saga-checkout.md` §T5, ADR 0010, pendências das auditorias 0024/0025.

## Escopo

Métricas, 5 alertas, topologia da fila nova, módulo `notificacao` (stub), teste ponta a ponta, tratamento de "já estornado" no adapter Stripe e correção do nome do histograma do gateway.

## Fora de escopo

- E-mail com QR e token HMAC (E7).
- Replay da DLQ, gauge de profundidade das DLQs novas, limpeza da outbox publicada, prefetch e health do worker (0027).
- Tempo e dashboards de negócio (E10).
- **Limpeza de pedidos abandonados:** vai para o E11, junto com a de holds. A trilha `pedido_eventos` tem retenção de 12 meses (doc.md §7) e FK para `pedidos`, então apagar pedidos em 30 dias apagaria a trilha. Desvio do refinamento (SRE), registrado aqui.
- Varredura de pedidos `expirado` cuja cobrança acabou aprovada (auditoria 0025, NB2): mitigado pelo reenvio do webhook por 3 dias. Fica para a 0027, que precisa de uma coluna de "cobrança encerrada".

## Requisitos funcionais

- RF01 — `saga_compensacoes_total{passo}`:
  - `cobranca`: o gateway não criou a cobrança; o pedido fica `falhou` e os assentos são devolvidos.
  - `estorno`: o estorno foi concluído.
  - Conta só quando a compensação acontece de fato: CAS vencido, e não perdido para outro caminho.
- RF02 — `pedidos_presos{estado}` (gauge lido do banco a cada coleta):
  - `aguardando_vencido`: pendentes com `expira_em` além da margem do hold, que a reconciliação deveria ter resolvido.
  - `estorno_pendente`.
- RF03 — `checkout_confirmado_ate_notificado_segundos` (histograma, sem `WithUnit`): instante do evento `pedido.confirmado` (timestamp AMQP) até a notificação processada. É o SLI do checkout fim a fim.
- RF04 — Funil ganha a etapa `expirado`. Abandono não conta como compensação.
- RF05 — 5 regras de alerta (Grafana → Discord), de 9 para 14:
  - estorno executado (warning);
  - estorno pendente há 30 min (critical);
  - pedidos vencidos não reconciliados há 10 min (critical);
  - breaker do gateway aberto (warning);
  - p95 de confirmação → notificação > 5 s (warning).
- RF06 — Topologia: quorum queue `notificacao.pedido_confirmado` ligada a `pedido.confirmado` na `morfeu.events`, `x-delivery-limit=3`, com **DLX própria** (`notificacao.pedido_confirmado.dlx`, fanout) → DLQ. A DLX de filmes é fanout e espalharia as mensagens mortas entre as DLQs. A mudança é aditiva e não troca o tipo de exchange existente (evita 406).
- RF07 — Módulo `notificacao`:
  - O consumidor aplica o efeito na TX do dedup (`processed_messages`).
  - Payload sem `pedido_id` válido → erro permanente (DLQ direto).
  - A entrega (`Entregar`, injetada) falhando → erro transitório: redelivery, depois DLQ.
  - Nunca toca o pedido.
  - Sem `Entregar` é stub, que só registra e mede a latência. O E7 injeta o e-mail.
- RF08 — `broker.Entrega` e `outbox.Mensagem` ganham `OccurredAt` (timestamp AMQP já publicado pelo relay).
- RF09 — Adapter Stripe: `charge_already_refunded` no estorno é **sucesso**. Depois de 24 h a chave de idempotência expira e repetir o estorno de um pagamento já devolvido responde assim (auditoria 0025).
- RF10 — Correção da 0024: `gateway_duration_seconds` sem `WithUnit("s")`. O exporter anexaria `_seconds` ao nome, gerando `_seconds_seconds`.

## Requisitos não funcionais

- RNF01 — O `notificacao` não importa `pedido` nem o cliente AMQP (depguard `notificacao-domain`).
- RNF02 — Labels só de conjuntos fixos (`passo`, `estado`, `etapa`). Nunca `pedido_id`.
- RNF03 — Teste ponta a ponta com PG, Redis e RabbitMQ reais, relógio real e prazo explícito para o assíncrono. Sem sleep fixo.

## Regras de negócio

- RN01 — A notificação é pós-pivô: sua falha nunca estorna nem cancela a venda (doc.md §10).

## Critérios de aceite

- [ ] CA01 — Gateway fora → `saga_compensacoes_total{passo=cobranca}` = 1. Estorno concluído → `{passo=estorno}` = 1.
- [ ] CA02 — `pedidos_presos`: vencido só conta depois da margem; estorno pendente conta até concluir; depois das tarefas, 0/0.
- [ ] CA03 — Stack de observabilidade com 14 regras provisionadas.
- [ ] CA04 — Consumidor: entrega + latência; falha de entrega transitória; payload inválido permanente; stub sem `Entregar`.
- [ ] CA05 — Ponta a ponta:
  - trava (HTTP) → pedido (HTTP) → webhook assinado → `pago` com 2 ingressos;
  - relay → RabbitMQ → consumidor entrega;
  - 1 registro de dedup para o evento do pedido e a latência medida.
- [ ] CA06 — Entrega sempre falhando → mensagem na DLQ própria; pedido segue `pago` com o ingresso.
- [ ] CA07 — "Já estornado" do Stripe → sucesso.
- [ ] CA08 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Consumidor stub/entrega/erros | unit | CA04 |
| Adapter Stripe "já estornado" | unit | CA07 |
| Compensações e presos com PG real | integração (`pedido`) | CA01, CA02 |
| Provisionamento do Grafana | integração (stack) | CA03 |
| Jornada da API com PG + Redis + RabbitMQ reais | integração (`test/checkout`) | CA05, CA06 |

## Plano de implementação

1. `OccurredAt` na entrega; fila + DLX/DLQ próprias.
2. `notificacao` (stub) + consumidor no worker + histograma.
3. Compensações, presos, funil `expirado`; métricas no `main`; correção do histograma do gateway.
4. Alertas + teste da stack.
5. Teste ponta a ponta; "já estornado".
6. Lint; `-race`; gate.

**Skills de apoio (§4.4):** `golang-observability-opentelemetry`, `golang-testing`.

## Arquivos que serão criados

- `internal/notificacao/consumidor.go`, `internal/notificacao/consumidor_test.go`
- `test/checkout/checkout_integration_test.go`
- `docs/tasks/0026-observabilidade-saga.md`, `docs/prd/0026-observabilidade-saga.md`

## Arquivos que serão modificados

- `internal/broker/{client.go, consumer.go}`, `internal/outbox/dedup.go`
- `internal/pedido/{queries.sql, service.go, pivo.go, tarefas.go, handler_test.go, tarefas_test.go}`, `internal/pedido/db/queries.sql.go` (gerado), `internal/pedido/pagamento/{stripe.go, stripe_test.go}`
- `cmd/morfeu/main.go`, `.golangci.yml`, `configs/grafana/provisioning/alerting/alertas.yml`, `test/observabilidade/stack_integration_test.go`
- `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total: 25.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- Nova fila e duas exchanges no broker (declaração idempotente no boot, aditiva).
- O gauge `pedidos_presos` faz uma consulta por coleta, sobre índices parciais.

## Riscos

- Mudança de topologia em broker já existente: só acrescenta, não altera argumentos das filas antigas.

## Estratégia de rollback

Reverter o merge. As filas novas ficam órfãs no broker, sem efeito, e podem ser removidas pela UI de management.
