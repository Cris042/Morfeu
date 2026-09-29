# PRD 0006 — Instrumentação da app: OTel + /metrics + logs correlacionados (E0d, parte 1/2)

- **Task:** docs/tasks/0006-instrumentacao-otel-metricas.md
- **Branch:** feature/0006-instrumentacao-otel-metricas
- **Data:** 2026-09-29
- **Status:** concluído

## Objetivo

Tornar o binário observável sem depender da stack de infra (0007): traces OTel com sampling e exporter descartável explícito, `/metrics` Prometheus com golden signals por rota roteada + métricas de mensageria, logs JSON correlacionados (`trace_id`/`span_id`) e redação de campos sensíveis nascendo no logger. Fonte: refinamento E0 §"Task E0d" (parte app), ADR 0003 (instrumentação só em plataforma), ADR 0007 (traceparent no envelope).

## Escopo

- `internal/telemetria` (plataforma): TracerProvider + MeterProvider (exporter Prometheus do OTel num `prometheus.Registry` próprio), handler de `/metrics`, shutdown ordenado; gauges observáveis de mensageria.
- Middleware `otelecho` (traces + métrica `http.server.request.duration` com `http.route`), `otelpgx` no pool.
- Consumer continua o trace do produtor a partir do header `traceparent` (span por entrega).
- Logger: core de redação + helper de correlação com o span do `ctx`.

## Fora de escopo

Stack de infra (Prometheus/exporters/Loki/Alloy/Grafana/alertas — task 0007); exporter OTLP e Tempo (E6/E10); tail sampling; métricas de negócio; SLOs; persistir `traceparent` na outbox (o relay segue gerando um por publicação — a continuidade é produtor→consumidor).

## Requisitos funcionais

- RF01 — `telemetria.Iniciar(ctx, cfg) (*Telemetria, error)`: TracerProvider com sampler `ParentBased(TraceIDRatioBased(OTEL_TRACES_SAMPLER_ARG, padrão 0.1))`, resource `service.name=morfeu` + `service.version`, span processor com **exporter de descarte explícito** (tipo próprio que ignora spans); registrado como global junto com o propagator W3C (`TraceContext`). `Shutdown(ctx)` encerra ambos os providers.
- RF02 — MeterProvider com `otel/exporters/prometheus` num registry dedicado (sem o default global do processo); `Telemetria.Handler()` = `promhttp.HandlerFor(registry)`; rota `GET /metrics` no Echo (mesma porta; a exposição pública é decidida na E0c-CD/0007 — Caddy não roteia `/metrics`).
- RF03 — Golden signals: `otelecho.Middleware("morfeu", WithTracerProvider, WithMeterProvider)` → histograma `http_server_request_duration_seconds` com labels `http_route` (template roteado; 404 sem rota não gera série por path), método e status. `/metrics` e `/health` ficam fora dos spans/métricas (skipper) para não poluir.
- RF04 — Mensageria (gauges observáveis, lidos no scrape): `morfeu_outbox_pendentes` (`outbox.Pendentes`), `morfeu_outbox_lag_segundos` (idade do pendente mais antigo; 0 se nenhum — nova query `IdadePendenteMaisAntigo`), `morfeu_dlq_mensagens{fila}` (profundidade via `broker.Client.ProfundidadeFila` — declaração passiva; só registrado com broker). Falha na leitura → observação omitida + log `warn`, nunca pânico.
- RF05 — `otelpgx.NewTracer()` no `pgxpool.Config.ConnConfig.Tracer`.
- RF06 — Consumer: `broker.processar` extrai `traceparent` via propagator global e abre span `consumir <fila>` (kind consumer) filho do produtor; o handler recebe esse `ctx`.
- RF07 — Logger: `logger.NewLogger` embrulha o core com redação — campos cujo nome (case-insensitive) contém `senha`, `password`, `token`, `authorization`, `secret`, `pan`, `cartao`/`card` têm o valor substituído por `[REDACTED]`. `logger.ComTrace(ctx, *zap.Logger) *zap.Logger` adiciona `trace_id`/`span_id` quando há span válido no ctx. Usado no request logger do Echo e no `outbox.NovoHandler`.

## Requisitos não funcionais

- RNF01 — Cardinalidade: labels permitidos por lista fechada; **proibido** qualquer label de id de assento/sessão/usuário ou path bruto — teste que coleta o registry após tráfego sintético (incluindo 404 com paths aleatórios) e falha em label fora da allowlist.
- RNF02 — Fronteiras (ADR 0003): domínio (`catalogo`) não importa OTel/Prometheus; instrumentação só em `telemetria`, `broker`, `logger`, `outbox` e `main`.
- RNF03 — Deps novas registradas no `lib.md` antes do import; govulncheck limpo; versões OTel alinhadas (core `v1.46.0`).
- RNF04 — Custo: exporter de descarte = zero I/O de traces; gauges calculados só no scrape (sem goroutine extra).

## Regras de negócio

N/A — task de plataforma, sem regra de domínio. (Justificativa: nenhum comportamento observável ao cliente muda; só telemetria.)

## Critérios de aceite

- [ ] CA01 — Após `GET /filmes` e `GET /nao-existe-<uuid>`, `/metrics` contém `http_server_request_duration_seconds_count{...http_route="/filmes"...}` e nenhuma série com o path aleatório.
- [ ] CA02 — Teste de cardinalidade: todos os labels do registry ∈ allowlist; falha se label proibido surgir (provado com instrumento de teste que viola, dentro do próprio teste).
- [ ] CA03 — Integração (PG+RabbitMQ reais): com N pendentes na outbox e M mensagens na DLQ, `/metrics` mostra `morfeu_outbox_pendentes` ≥ N, `morfeu_outbox_lag_segundos` > 0 e `morfeu_dlq_mensagens{fila="catalogo.filme_criado.dlq"}` = M.
- [ ] CA04 — Log emitido via `ComTrace` dentro de um span traz `trace_id` = `span.SpanContext().TraceID()`.
- [ ] CA05 — Consumer: span da entrega tem o mesmo trace ID do `traceparent` recebido (tracetest in-memory).
- [ ] CA06 — Redação: `zap.String("Authorization", "Bearer x")`, `zap.String("senha","y")` saem `[REDACTED]`; campos comuns intactos.
- [ ] CA07 — Sampler configurado `ParentBased(TraceIDRatioBased(0.1))`; exporter de descarte registrado (teste unitário do setup).
- [ ] CA08 — Regressão: suíte existente inalterada; CI verde (lint, -race, govulncheck, sqlc).

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Echo com middleware + `/metrics`; requests `/filmes` e 404 aleatório | unit (httptest, handler fake) | CA01, RF03 |
| Coleta do registry vs allowlist; instrumento violador detectado | unit | CA02, RNF01 |
| Outbox pendente + DLQ com mensagens → scrape | integração (PG+RabbitMQ) | CA03, RF04 |
| `ComTrace` com tracer SDK real | unit | CA04 |
| `processar` com traceparent conhecido → span com mesmo trace ID | integração (RabbitMQ, tracetest) | CA05, RF06 |
| Redação por nome de campo | unit | CA06, RF07 |
| Setup: sampler/descrição, Shutdown idempotente | unit | CA07, RF01 |

## Plano de implementação

1. Registrar deps no `lib.md`; `go get` (otel/sdk, sdk/metric, exporters/prometheus, otelecho v0.70.0, otelpgx, client_golang).
2. `internal/telemetria` (RF01/RF02/RF04) + testes.
3. Logger (RF07) + testes.
4. `broker` (RF06 + `ProfundidadeFila`), `outbox` (query de lag + `Lag`), `NovoHandler` com `ComTrace`.
5. `main.go`: Iniciar telemetria, otelpgx, otelecho, `/metrics`, gauges, shutdown.
6. Integração (CA03/CA05), lint, -race.

**Skills de apoio (§4.4):** `golang-observability-opentelemetry`, `golang-testing`.

## Arquivos que serão criados

- `internal/telemetria/telemetria.go` — setup/shutdown, exporter de descarte, handler.
- `internal/telemetria/mensageria.go` — gauges de outbox/DLQ.
- `internal/telemetria/telemetria_test.go` — CA01, CA02, CA07.
- `internal/logger/redacao.go` — core de redação + `ComTrace`.
- `internal/outbox/telemetria_integration_test.go` — CA03, CA05.
- `docs/prd/0006-instrumentacao-otel-metricas.md` — este PRD.

## Arquivos que serão modificados

- `internal/logger/logger.go`, `internal/logger/logger_test.go` — core embrulhado; CA04, CA06.
- `internal/broker/consumer.go` — span por entrega (RF06).
- `internal/broker/client.go` — `ProfundidadeFila`.
- `internal/outbox/queries.sql`, `internal/outbox/db/queries.sql.go`, `internal/outbox/outbox.go` — `Lag`.
- `internal/outbox/dedup.go` — log com `ComTrace`.
- `cmd/morfeu/main.go` — wiring.
- `go.mod`, `go.sum`, `lib.md`.
- `docs/tasks/0006-…`, `docs/tasks/README.md`, `plan.md`, `state.md`, `docs/roadmap.md`.

Total previsto: ~24.

## Dependências utilizadas

Novas (registrar no `lib.md` antes do import):
- `go.opentelemetry.io/otel/sdk` + `sdk/metric` **v1.46.0** (core `otel`/`trace`/`metric` sobem de 1.41 → 1.46 junto).
- `go.opentelemetry.io/otel/exporters/prometheus` **v0.68.0** (traz `prometheus/client_golang` v1.24.1, usado direto para `Registry`/`promhttp`).
- `go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho` **v0.70.0** — decisão: a v0.71.0 marca o módulo como *deprecated* em favor de `github.com/labstack/echo-otel/v4`, publicado em 2026-09-28 (v4.0.0, 1 dia de vida). Fica a v0.70.0 (última não-deprecated, estável); **débito registrado**: migrar para `echo-otel/v4` quando tiver ≥ 1 patch release e adoção — revisitar na 0007 ou E1.
- `github.com/exaring/otelpgx` **v0.12.0** (pgx v5.9.2, compatível).

## Impactos técnicos

- Nova rota `GET /metrics` (interna; exposição pública bloqueada no proxy — E0c-CD).
- Leve overhead por request (middleware) e por query (tracer pgx) — spans não amostrados são non-recording.
- `broker` e `outbox` passam a importar OTel API (já importavam `propagation`).

## Riscos

- Cardinalidade → allowlist + teste (CA02) + skipper de `/metrics`/`/health`.
- Deprecação do otelecho → débito explícito com gatilho de revisão.
- "100% dos erros" do refinamento exige tail sampling (decisão no fim do trace) — incompatível com head sampling puro e sem sentido com exporter de descarte. **Desvio registrado**: fica para quando entrar o collector/Tempo (E6/E10); hoje o `trace_id` existe em todo log (spans não amostrados ainda carregam IDs), o que já dá correlação de erro via logs.
- Bump do core OTel quebrar `propagation` da 0002 → suíte da 0002 roda no CI (regressão).

## Estratégia de rollback

Reverter o merge. Sem migration; nenhuma mudança de contrato além da rota nova `/metrics`.
