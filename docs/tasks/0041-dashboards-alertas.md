# Task 0041 — Dashboards de negócio, alertas e watchdog (E10, T2)

- **Data:** 2026-10-01
- **Status:** concluída (auditoria APROVADA em 2026-10-01)
- **Branch:** `feature/0041-dashboards-alertas` (sobre a 0040)
- **PRD:** docs/prd/0041-dashboards-alertas.md
- **Item do roadmap:** E10 — Observabilidade completa (2ª e última). Refinamento: `docs/refinamentos/E10-observabilidade.md` §T2.

## Objetivo

O negócio visível no Grafana (funil, compensações/cancelamentos, reservas, integrações), os alertas pendentes do E9 e o watchdog por heartbeat externo.

## Escopo

4 dashboards; alertas de purga parada e watchdog (+ contact point e rota do heartbeat); textos dos alertas de estorno; teste de contrato dashboards/alertas × métricas; lista nominal de regras no teste da stack; runbook.

## Fora de escopo

Monitor HTTP externo e aceite manual do watchdog (E0c-CD — exigem URL pública).

## Arquivos esperados

14 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

Ver PRD (CA01–CA06).

## Riscos

- URL do heartbeat vazando → env, nunca versionada; placeholder no example.

## Estimativa de impacto

Baixo: só configuração de observabilidade e testes.
