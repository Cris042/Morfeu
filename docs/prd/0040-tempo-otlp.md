# PRD 0040 — Traces: Tempo + OTLP + tail sampling no Alloy (E10, T1)

- **Task:** docs/tasks/0040-tempo-otlp.md
- **Branch:** feature/0040-tempo-otlp
- **Data:** 2026-10-01
- **Status:** concluído

## Objetivo

Materializar o ADR 0012. Fontes: `docs/refinamentos/E10-observabilidade.md` §T1, `doc.md` §13, ADR 0003 (OTel só na plataforma).

## Escopo

App (exporter + sampler + sanitização), stack (Tempo, Alloy, Grafana) e testes.

## Fora de escopo

- Dashboards, alertas e watchdog (0041).
- Exemplars (dependem de OpenMetrics no `/metrics` — best effort, fora desta task).
- Tempo 3.x (muda o modelo de armazenamento — avaliação à parte).

## Requisitos funcionais

- RF01 — `OTEL_EXPORTER_OTLP_ENDPOINT` (config) liga a exportação: sampler `ParentBased(AlwaysSample)` + `BatchSpanProcessor` (fila 2048, lote 512, intervalo 2 s, timeout 3 s) com `otlptracehttp` (timeout 2 s, sem retry). Vazio → amostragem de cabeça 10% + descarte (comportamento anterior).
- RF02 — Sanitização antes de exportar (`telemetria.Sanitizar`): `url.path` vira `http.route` quando há rota; somem `url.query`, `url.full`, `http.url`, `http.target`, `http.request.header.*`, `http.response.header.*`, `db.query.parameter.*`, `enduser.*`, `user.*` e — dados pessoais (LGPD) — `client.address`, `client.port`, `network.peer.address`, `user_agent.original`.
- RF03 — Tempo `2.10.4` monolítico: receptor OTLP gRPC interno, storage local, `block_retention` 72 h, limites de ingestão e de tamanho de trace, 384 MB, volume `morfeu-tempo`, **sem porta publicada**.
- RF04 — Alloy: `otelcol.receiver.otlp` (HTTP 4318, interno) → `memory_limiter` (400 MiB) → `attributes` (mesma remoção do RF02) → `tail_sampling` (ERROR; latência > 300 ms; 10% probabilístico; `decision_wait` 10 s; `num_traces` 20 000) → `batch` → Tempo; Alloy 256 → 512 MB.
- RF05 — Compose de observabilidade injeta `OTEL_EXPORTER_OTLP_ENDPOINT=http://alloy:4318` no `app`.
- RF06 — Grafana: datasource `tempo`; Loki com derived field `trace_id` → Tempo; Tempo → logs do `app` pelo `trace_id`.

## Requisitos não funcionais

- RNF01 — A exportação nunca bloqueia a requisição; o app sobe e atende com o coletor fora.
- RNF02 — Domínio continua sem OTel (ADR 0003); dependência nova no `lib.md`; govulncheck sem vulnerabilidade alcançável.

## Critérios de aceite

- [x] CA01 — Com coletor (falso, `httptest`), o span chega com rota templada e `pedido_id`, e **sem** token do ingresso, e-mail, Authorization, `url.query`/`url.full`.
- [x] CA02 — Coletor inacessível: 10 240 spans criados em < 1 s; shutdown dentro do prazo.
- [x] CA03 — Sem rota, `url.path` é mantido; prefixos proibidos caem sempre.
- [x] CA04 — `alloy validate` da config real passa.
- [x] CA05 — Stack real (Alloy + Tempo com as configs do repo): trace com erro e trace lento chegam ao Tempo pelo tail sampling.
- [x] CA06 — Grafana provisiona o datasource `tempo`; nenhuma porta além do Grafana em 127.0.0.1.
- [x] CA07 — Lint, suíte `-race` e CI verdes; `doc.md` §13 e `lib.md` atualizados.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Coletor falso, coletor fora, sanitização | unit (`telemetria`) | CA01–CA03 |
| `alloy validate`, Alloy→Tempo, datasources, portas | integração (`test/observabilidade`) | CA04–CA06 |

## Plano de implementação

1. Dep `otlptracehttp` + `exportacao.go` + config/main.
2. Tempo, Alloy, Grafana, compose.
3. Testes; `lib.md`, `doc.md`.

**Skills de apoio (§4.4):** `golang-observability-opentelemetry`, `security-review`.

## Arquivos que serão criados

- `internal/telemetria/exportacao.go`, `internal/telemetria/exportacao_test.go`, `configs/tempo/tempo.yml`
- `docs/tasks/0040-tempo-otlp.md`, `docs/prd/0040-tempo-otlp.md`

## Arquivos que serão modificados

- `internal/telemetria/telemetria.go`, `internal/config/config.go`, `cmd/morfeu/main.go`, `go.mod`, `go.sum`
- `configs/alloy/config.alloy`, `configs/grafana/provisioning/datasources/datasources.yml`, `docker-compose.observability.yml`, `test/observabilidade/stack_integration_test.go`
- `lib.md`, `doc.md`, `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 19.

## Dependências utilizadas

`otlptracehttp` v1.46.0 (nova — `lib.md`); `grafana/tempo:2.10.4` (imagem nova).

## Impactos técnicos

- Com a stack de observabilidade, todo span do app é exportado (mais CPU/rede local); medir no E12.
- `google.golang.org/grpc` sobe para 1.83.1 (indireto).

## Riscos

- Alloy sem memória sob carga → `memory_limiter` + `num_traces`.
- Trace incompleto quando partes assíncronas passam de 10 s (documentado no ADR 0012).

## Estratégia de rollback

Remover `OTEL_EXPORTER_OTLP_ENDPOINT` do compose (o app volta ao descarte) ou reverter o merge.
