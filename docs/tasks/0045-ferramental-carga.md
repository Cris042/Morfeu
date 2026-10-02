# Task 0045 — Ferramental de carga (E12, T1)

- **Data:** 2026-10-01
- **Status:** em implementação
- **Branch:** `feature/0045-ferramental-carga`
- **PRD:** docs/prd/0045-ferramental-carga.md
- **Item do roadmap:** E12 — Teste de carga k6 (1ª de 2). Refinamento: `docs/refinamentos/E12-teste-de-carga.md` §T1.

## Objetivo

Deixar pronto tudo que a execução do M5 precisa, sem abrir brecha em produção.

## Escopo

Flag `MORFEU_LOADTEST` com guardas, limites por IP ×1000, gauge + alerta, rota de teste fora de produção, buckets com 0,3 s, `deploy/carga/` (compose, seed, invariante, k6, smoke), `docs/carga/` (queries, runbook), `lib.md`.

## Fora de escopo

Execução e relatório (0046); `workflow_dispatch`; single-flight/worker/Argon2.

## Arquivos esperados

≤ 30 (lista no PRD).

## Dependências esperadas

Imagem `grafana/k6` 2.3.0 por digest (ferramenta; sem dependência Go nova).

## Critérios de aceite

Ver PRD (CA01–CA08).

## Riscos

- Modo de carga ligado em produção → guardas de boot + gauge/alerta.

## Estimativa de impacto

Médio: config e montagem do app (atrás da flag), telemetria (buckets) e ferramental novo isolado.
