# ADR 0012 — Amostragem de traces por tail sampling no Alloy

- **Status:** aceito
- **Data:** 2026-10-01
- **Task/PRD relacionados:** refinamento E10 (`docs/refinamentos/E10-observabilidade.md`), tasks 0040–0041; `doc.md` §9/§13 (traces "10% + 100% erros")

## Contexto

O `doc.md` fixou traces com "sampling 10% + 100% erros" e o Tempo entrando com a saga. Desde o E0d o app amostra na **cabeça** (`ParentBased(TraceIDRatioBased(0.1))`) e exporta para um descarte. Amostragem na cabeça decide no primeiro span — antes de saber se a requisição falhou ou demorou —, então "100% dos erros" é impossível nela: 90% dos traces de erro somem. Os 5 agentes do refinamento E10 convergiram em mover a decisão para o coletor; o usuário autorizou este ADR.

## Escopo

Cobre: onde e como os traces são amostrados, o caminho app → coletor → Tempo e os limites de recurso desse caminho. Não cobre: dashboards e alertas (0041), métricas (inalteradas), logs.

## Decisão

**O app exporta 100% dos traces por OTLP/HTTP para o Alloy, que decide por tail sampling (erros, lentos e 10% aleatório) e envia ao Tempo.**

- **App:** sampler `ParentBased(AlwaysOn)` quando há coletor configurado (`OTEL_EXPORTER_OTLP_ENDPOINT`); a taxa de cabeça continua configurável como plano B (e é o comportamento sem coletor, com o exporter de descarte de hoje). Exportação em lote, fila limitada, timeout curto, sem bloquear a requisição; o app sobe e atende com o coletor fora do ar. Exporter **`otlptracehttp`** (sem gRPC no binário), só em `internal/telemetria` (ADR 0003).
- **Alloy:** `otelcol.receiver.otlp` (HTTP, só na rede interna) → `memory_limiter` → `otelcol.processor.tail_sampling` com 3 políticas: `status_code = ERROR`, `latency > 300 ms` (o SLO do E12) e `probabilistic 10%`; `decision_wait` curto (10 s) e `num_traces` limitado → `otelcol.exporter.otlp` (gRPC interno) → Tempo. Memória do Alloy 256 → 512 MB. Defesa em profundidade: o Alloy remove atributos de URL/headers que possam carregar credenciais (a sanitização primária é no app).
- **Tempo:** monolítico, storage local, retenção 72 h, ~384 MB, sem porta publicada.

## Tecnologias ou padrões envolvidos

OpenTelemetry (SDK Go, OTLP), Grafana Alloy (`otelcol.*`), Grafana Tempo, tail-based sampling.

## Benefícios

- Cumpre o requisito do `doc.md` de verdade: 100% dos erros e dos lentos, 10% do resto.
- Política de amostragem muda sem rebuild do app (config do Alloy).
- O Alloy já está na stack (logs): nenhum serviço novo além do Tempo.

## Trade-offs

- O app passa a exportar todos os spans (mais CPU/rede local; spans do otelpgx em volume sob carga).
- O Alloy guarda traces em memória durante a janela de decisão (mais memória; risco de OOM sem limitador).
- Spans que chegam depois da decisão (partes assíncronas da saga além de `decision_wait`) ficam órfãos ou incompletos.

## Riscos

- **Alloy sob carga do E12 perdendo traces ou estourando memória** — prob. média, impacto baixo (só observabilidade). Mitigação: `memory_limiter`, `num_traces`, medição no E12 antes de aumentar limites.
- **Trace incompleto da saga assíncrona** — prob. média, impacto baixo. Mitigação: `decision_wait` ajustável; se o p99 da saga passar da janela, ligar etapas por span link.
- **Vazamento de credencial em atributo de span** — prob. baixa, impacto alto. Mitigação: sanitização no app + teste + remoção no Alloy; Tempo sem porta exposta.

## Estratégias para minimizar os trade-offs

- Exportação em lote com fila limitada (descarta em vez de bloquear) e métrica/log de descarte.
- Retenção curta no Tempo e volume coberto pelo alerta de disco.
- Plano B documentado: sem coletor, volta a amostragem de cabeça com descarte.

## Condições de reversão

Revisitar se: (a) o custo do exporter a 100% aparecer no p95 do E12; (b) o Alloy precisar de mais memória do que a VM comporta; (c) a saga virar assíncrona longa (traces sempre incompletos) — aí head sampling por rota + links.

## Impacto esperado

`internal/telemetria` (exporter OTLP, sampler por config), `lib.md` (`otlptracehttp` v1.46.0), compose de observabilidade (Tempo, Alloy com receptor OTLP e 512 MB), datasources do Grafana (Tempo; Loki → Tempo pelo `trace_id`), `doc.md` §13.

## Alternativas consideradas e descartadas

- **Manter head sampling 10% no app:** simples, mas descarta 90% dos erros — contraria o requisito.
- **OpenTelemetry Collector contrib como container separado:** mesmo processador, mais um serviço; o Alloy já roda e traz o componente.
- **Tempo metrics-generator / Jaeger / Grafana Cloud:** excesso (as métricas RED já existem), segunda UI, ou dados fora da VM (contra a decisão self-hosted).
- **OTLP/gRPC no app:** puxa `google.golang.org/grpc` para o binário sem ganho no volume do projeto.
