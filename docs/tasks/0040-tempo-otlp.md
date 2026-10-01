# Task 0040 — Traces: Tempo + OTLP + tail sampling no Alloy (E10, T1)

- **Data:** 2026-10-01
- **Status:** em andamento
- **Branch:** `feature/0040-tempo-otlp` (sobre `docs/E10-refinamento`)
- **PRD:** docs/prd/0040-tempo-otlp.md
- **Item do roadmap:** E10 — Observabilidade completa (1ª de 2). Refinamento: `docs/refinamentos/E10-observabilidade.md` §T1. ADR 0012.

## Objetivo

Traces da saga visíveis no Grafana: o app exporta por OTLP/HTTP, o Alloy decide (erros, lentos, 10%) e o Tempo guarda 72 h; log ↔ trace pelo `trace_id`.

## Escopo

Exporter OTLP sanitizado no `internal/telemetria`; Tempo no compose de observabilidade; pipeline de traces no Alloy; datasources e correlação no Grafana; testes do exporter, da sanitização e da stack.

## Fora de escopo

Dashboards de negócio, alertas novos e watchdog (0041); exemplars (best effort — não entram).

## Arquivos esperados

~20 (lista no PRD).

## Dependências esperadas

`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` v1.46.0; imagem `grafana/tempo:2.10.4`.

## Critérios de aceite

Ver PRD (CA01–CA07).

## Riscos

- Credencial em atributo de span → sanitização no app + remoção no Alloy + teste.

## Estimativa de impacto

Médio: novo serviço na stack de observabilidade; o app passa a exportar todos os spans quando há coletor.
