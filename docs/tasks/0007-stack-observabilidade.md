# Task 0007 — Stack de observabilidade: Prometheus + exporters + Alloy/Loki + Grafana + alertas (E0d, parte 2/2)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `chore/0007-stack-observabilidade`
- **PRD:** docs/prd/0007-stack-observabilidade.md — criar antes de implementar (just-in-time, §6.2.5)
- **Item do roadmap:** E0d — observabilidade base (parte 2/2; divisão registrada na task 0006). Fecha o E0 exceto a E0c-CD (bloqueada pela VM Oracle).

## Objetivo

Subir a stack self-hosted que consome o `/metrics` e os logs JSON entregues na 0006: Prometheus com os exporters da lista fechada, Alloy → Loki com retenção limitada, Grafana provisionado por arquivo (2–3 dashboards fechados) e alertas mínimos → Discord. Tudo reconstituível por código e leve para a VM A1.

## Escopo

Conforme refinamento E0 §"Task E0d" (parte infra):

- `docker-compose.observability.yml` separado (incluído condicionalmente: `docker compose -f docker-compose.yml -f docker-compose.observability.yml`), rede interna; **só o Grafana publica porta** (localhost em dev).
- Prometheus: scrape do app (`/metrics`) + node_exporter, cAdvisor, postgres_exporter (usuário `pg_monitor` mínimo), redis_exporter, `rabbitmq_prometheus` (plugin nativo). Imagens multi-arch (ARM64) pinadas.
- Serviço do app no compose de dev (profile `app`), para que Prometheus e Alloy o enxerguem e para exercitar `depends_on: service_healthy` (exigência herdada da 0005).
- Alloy → Loki: coleta de logs dos containers; Loki com retenção de 14 dias (compactor) + limites de ingestão; o disco é protegido pelo alerta nº 1.
- Grafana: datasources e 3 dashboards por arquivo (golden signals da API; USE da VM + PG/Redis/RabbitMQ; outbox/DLQ); sem senha literal em YAML; anônimo e sign-up desligados.
- Alertas (lista fechada): disco > 80%, API fora, erro acima do limite, DLQ crescendo, consumidor parado, saturação de conexões do PG → contact point Discord (webhook via env).
- Smoke declarativo: teste de integração que sobe Grafana/Prometheus/Loki com a config real e verifica provisionamento (dashboards, datasources, regras) e validade das configs.

## Fora de escopo

Tempo/traces (E6/E10); deploy na VM e watchdog externo apontando para URL pública (E0c-CD — procedimento documentado aqui); SLOs formais (E10/E12); dashboards além dos 3.

## Arquivos esperados

~25: compose de observabilidade (1) · compose dev (1) · configs Prometheus/Loki/Alloy (3) · plugins RabbitMQ (1) · init do usuário de monitoração do PG (1) · Grafana provisioning (datasources, dashboards provider, alerting — 3) + 3 dashboards JSON · `.env.docker-compose.example` (1) · Makefile (1) · teste de integração da stack (1) · `docs/observabilidade.md` runbook (1) · `lib.md` (1) · controle (task/README/prd/plan/state/roadmap — 6).

## Dependências esperadas

Imagens novas (registrar no `lib.md`, tabela de infraestrutura, com tag pinada e ARM64 verificado): prom/prometheus v3.15.0, grafana/grafana 13.2.3, grafana/loki 3.7.8, grafana/alloy v1.20.1, prom/node-exporter v1.12.1, ghcr.io/google/cadvisor v0.60.6, prometheuscommunity/postgres-exporter v0.20.1, oliver006/redis_exporter v1.92.1. Nenhuma dependência Go nova.

## Critérios de aceite

- [ ] `docker compose -f docker-compose.yml -f docker-compose.observability.yml --profile app up` sobe tudo saudável; todos os targets do Prometheus `up == 1`.
- [ ] Grafana provisionado: 3 dashboards e datasources Prometheus/Loki via API; sem login anônimo; senha só via env.
- [ ] Logs do app consultáveis no Loki (label de container/serviço) com `trace_id`.
- [ ] 6 regras de alerta provisionadas com contact point Discord (webhook via env).
- [ ] Nenhuma porta de PG/Redis/RabbitMQ/Prometheus/Loki/exporters publicada pelo compose de observabilidade (só Grafana, em 127.0.0.1).
- [ ] Teste de integração da stack verde no CI.

## Riscos

- Memória na A1 → limites de memória por serviço + retenção curta; cAdvisor/Alloy com intervalos conservadores.
- Socket do Docker montado (Alloy/cAdvisor) → somente leitura, documentado; nunca exposto.
- Cardinalidade de cAdvisor/node_exporter → coletores desnecessários desligados.
- Teste de stack pesado no CI → subir só Grafana/Prometheus/Loki, não o compose inteiro.

## Estimativa de impacto

Médio em infra (8 serviços novos, opcionais via arquivo separado), nenhum em código de produção, nenhum em banco (usuário de monitoração só em init de volume novo), nenhum em usuários.
