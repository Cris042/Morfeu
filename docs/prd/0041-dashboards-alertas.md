# PRD 0041 — Dashboards de negócio, alertas e watchdog (E10, T2)

- **Task:** docs/tasks/0041-dashboards-alertas.md
- **Branch:** feature/0041-dashboards-alertas
- **Data:** 2026-10-01
- **Status:** em andamento

## Objetivo

Fontes: `docs/refinamentos/E10-observabilidade.md` §T2 (decisão do usuário: watchdog por healthchecks.io agora); pendências do E9 registradas no `state.md` (alerta de purga, textos de estorno).

## Escopo

Configuração do Grafana (dashboards, alertas, contact point, rota), testes e runbook.

## Fora de escopo

- Monitor HTTP da URL pública e aceite manual do watchdog (E0c-CD).
- Métricas novas (dashboards só com métricas existentes — refinamento E10).

## Requisitos funcionais

- RF01 — 4 dashboards provisionados na pasta Morfeu, UIDs fixos, sem labels de alta cardinalidade: **funil do checkout** (pedidos por etapa, conversão 24 h, confirmado→e-mail p50/p95, holds vendidos); **compensações e cancelamentos** (compensações por passo, cancelamentos por origem, pedidos presos, estornos pendentes); **reservas e ocupação** (holds criados × recusados, expirados × vendidos, taxa de disputa, conflitos de horário); **gateway e e-mail** (requisições e p95 por operação, breaker, e-mails por resultado, p95 de envio).
- RF02 — Alerta **purga da trilha parada > 48 h** (`time() - max(auditoria_purga_ultima_execucao_timestamp)`); sem série = OK.
- RF03 — **Watchdog**: regra sempre disparando (label `watchdog=true`) roteada só para o contact point `heartbeat-externo` (webhook POST para `HEALTHCHECKS_PING_URL`, repetição 5 min); nunca ao Discord; placeholder no `.env.observability.example`.
- RF04 — Textos dos alertas `morfeu-saga-estorno` e `morfeu-saga-estorno-preso` cobrem cancelamentos (ADR 0011).
- RF05 — Lista fechada: 18 regras conferidas **por uid** no teste da stack; 7 dashboards; contact points Discord e heartbeat.
- RF06 — Teste de contrato: toda métrica do app citada em dashboards e alertas é declarada no código; dashboards com JSON válido e uid único.

## Requisitos não funcionais

- RNF01 — Nenhuma PII em dashboards/alertas; URL do heartbeat e webhook do Discord só por env.

## Critérios de aceite

- [ ] CA01 — Contrato dashboards × métricas e alertas × métricas verde; mutação (typo numa métrica) falha o teste.
- [ ] CA02 — Grafana real aceita o provisioning: 7 dashboards, 3 datasources, 18 regras por uid, 2 contact points.
- [ ] CA03 — Rota do watchdog só para o heartbeat (o watchdog não chega ao Discord).
- [ ] CA04 — Runbook (`docs/observabilidade.md`) com os alertas novos, o passo a passo do healthchecks.io e o que fica para a E0c-CD.
- [ ] CA05 — Pendências do E9 baixadas do `state.md`.
- [ ] CA06 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Contrato dashboards/alertas × métricas | integração estática (`test/observabilidade`) | CA01 |
| Provisionamento no Grafana real | integração (`test/observabilidade`) | CA02, CA03 |

## Plano de implementação

1. Dashboards; 2. alertas, contact point e rota; 3. testes; 4. runbook.

**Skills de apoio (§4.4):** `golang-observability-opentelemetry`.

## Arquivos que serão criados

- `configs/grafana/dashboards/{negocio-funil, negocio-compensacoes, negocio-ocupacao, negocio-integracoes}.json`, `test/observabilidade/contrato_test.go`
- `docs/tasks/0041-dashboards-alertas.md`, `docs/prd/0041-dashboards-alertas.md`

## Arquivos que serão modificados

- `configs/grafana/provisioning/alerting/alertas.yml`, `.env.observability.example`, `test/observabilidade/stack_integration_test.go`, `docs/observabilidade.md`
- `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total: 17.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- `.env.observability` existente precisa de `HEALTHCHECKS_PING_URL` (o contact point exige URL no boot — usar o placeholder).

## Riscos

- Watchdog silenciado por engano → descrição "não silenciar" + aviso do serviço externo.

## Estratégia de rollback

Reverter o merge.
