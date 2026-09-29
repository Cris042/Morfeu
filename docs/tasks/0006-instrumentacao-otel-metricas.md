# Task 0006 — Instrumentação da app: OTel + /metrics + logs correlacionados (E0d, parte 1/2)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `feature/0006-instrumentacao-otel-metricas`
- **PRD:** docs/prd/0006-instrumentacao-otel-metricas.md — criar antes de implementar (just-in-time, §6.2.5)
- **Item do roadmap:** E0d — observabilidade base (parte 1/2). Usuário confirmou em 2026-09-29: VM Oracle ainda não existe → E0c-CD segue bloqueada e a E0d avança (a CD fura a fila quando a VM sair).

## Divisão da E0d (roles.md §6.3)

O refinamento estimou 16–20 arquivos, mas a lista fechada (SDK OTel + métricas + redação de logs **e** compose de observabilidade, 5 exporters, Alloy/Loki, provisionamento Grafana com 2–3 dashboards, alertas → Discord) passa de 30 com testes e docs de controle. Divisão por fronteira natural:

- **0006 (esta) — lado da app (Go):** tudo que muda o binário.
- **0007 (próxima) — stack de observabilidade (infra):** `docker-compose.observability.yml`, Prometheus + exporters, Alloy → Loki (retenção 14d + tamanho), Grafana provisionado (2–3 dashboards), alertas mínimos → Discord + watchdog. Consome o `/metrics` e os logs JSON desta task.

O shell da SPA (antes "0006 candidata") recebe número próprio quando for aberto.

## Objetivo

Deixar o binário observável: traces OTel (sampling 10% + 100% erros, exporter noop/discard explícito — sem Tempo), `/metrics` Prometheus com golden signals por rota roteada + métricas de mensageria (outbox pendentes, lag do relay, profundidade da DLQ), logs JSON com `trace_id`/`span_id` correlacionados e redação de campos sensíveis nascendo no logger.

## Escopo

Conforme refinamento E0 §"Task E0d" (parte app):

- Pacote de plataforma de telemetria (setup/shutdown do TracerProvider e MeterProvider/registry Prometheus); domínio não conhece OTel.
- `otelecho` (middleware) + `otelpgx` (tracer do pool); propagação do `traceparent` do envelope AMQP no consumer (continua o trace do produtor).
- `/metrics`: golden signals HTTP por rota roteada (nunca path bruto); `outbox_pendentes`, lag do relay (idade do pendente mais antigo), profundidade da DLQ.
- **Teste estático de cardinalidade**: nenhum label proibido (seat/session/user id, path bruto).
- Logger: redação de campos sensíveis (senha, token, Authorization, PAN futuro) + `trace_id`/`span_id` nos logs com contexto.
- Testes: httptest + registry local (golden signals e métricas de mensageria em `/metrics`), correlação trace_id log == span, redação; asserts da E0a inalterados.

## Fora de escopo

Toda a stack de infra (0007); Tempo/exporter OTLP de traces (E6/E10); dashboards/alertas (0007); métricas de negócio; SLOs (E10/E12).

## Arquivos esperados

~20: `internal/telemetria/` (setup + métricas de mensageria, ~3) · `internal/logger` (redação + correlação + testes, ~3) · `cmd/morfeu/main.go` (1) · `internal/outbox` (lag/queries + gerado, ~3) · `internal/broker` (profundidade DLQ + traceparent no consumer, ~2) · testes de integração (~2) · `go.mod`/`go.sum` (2) · `lib.md` (1) · controle (task/README/prd/plan/state, 5).

## Dependências esperadas

Novas (registrar no `lib.md` antes do import, versões via Context7/OSV): OTel SDK (`sdk`, `sdk/metric`), exporter Prometheus do OTel ou `prometheus/client_golang`, `otelecho`, `otelpgx`. Decisão exata no PRD.

## Critérios de aceite

- [ ] `/metrics` expõe golden signals HTTP com label de rota roteada (`/filmes`), nunca path bruto; teste com httptest.
- [ ] `/metrics` expõe `outbox_pendentes`, lag do relay e profundidade da DLQ com valores verificados em integração.
- [ ] Teste estático de cardinalidade falha se surgir label proibido.
- [ ] Log emitido dentro de request/consumo traz `trace_id` igual ao do span ativo.
- [ ] Campos sensíveis redigidos no log (teste).
- [ ] Sampling 10% + 100% erros; exporter de traces noop/discard explícito.
- [ ] Regressão: `/health`, `GET /filmes`, suíte 0002/0005 inalteradas; CI verde (-race, lint, govulncheck).

## Riscos

- Explosão de cardinalidade → teste estático + rota roteada.
- Dependências OTel pesadas/CVE → Context7 + govulncheck antes do import; versões alinhadas ao `otel v1.41` já presente.
- Overengineering (instrumentar demais) → lista fechada de métricas do refinamento.

## Estimativa de impacto

Médio em código (plataforma nova de telemetria, middleware), nenhum em banco (queries de leitura), baixo em infra (endpoint `/metrics` interno), nenhum em usuários.
