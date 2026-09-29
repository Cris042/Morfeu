# PRD 0007 — Stack de observabilidade (E0d, parte 2/2)

- **Task:** docs/tasks/0007-stack-observabilidade.md
- **Branch:** chore/0007-stack-observabilidade
- **Data:** 2026-09-29
- **Status:** concluído

## Objetivo

Subir, por código e em arquivo separado, a stack self-hosted que consome o que a 0006 entregou (`/metrics` e logs JSON com `trace_id`): Prometheus com os exporters da lista fechada, Alloy → Loki com retenção limitada, Grafana provisionado (3 dashboards fechados) e 6 alertas → Discord. Fonte: refinamento E0 §"Task E0d" (parte infra), `doc.md` §13/§14 (observabilidade, hardening), ADR 0006 (testes com containers reais).

## Escopo

- `docker-compose.observability.yml` (override do compose base): prometheus, loki, alloy, grafana, node-exporter, cadvisor, postgres-exporter, redis-exporter. Rede interna `morfeu-network`; **somente o Grafana publica porta** (`127.0.0.1:3000`).
- `docker-compose.yml`: serviço `app` (profile `app`, build do Dockerfile, `-mode=all`) com `depends_on: condition: service_healthy` em postgres/redis/rabbitmq (exigência herdada da 0005); RabbitMQ com plugin `rabbitmq_prometheus` (`enabled_plugins` montado); Postgres com script de init que cria o usuário de monitoração `morfeu_monitor` (`pg_monitor`).
- Configs versionadas em `configs/`: Prometheus (scrape + intervalos), Loki (single binary, filesystem, TSDB v13, compactor com retenção 14d, limites de ingestão), Alloy (discovery de containers → Loki), Grafana (datasources, provider de dashboards, alerting: contact point Discord, política, 6 regras) e 3 dashboards JSON.
- Segredos só via `.env.observability` (não versionado; `.env.observability.example` versionado).
- Teste de integração da stack + runbook `docs/observabilidade.md`.

## Fora de escopo

Tempo/traces (E6/E10); watchdog externo e Grafana na VM (E0c-CD — procedimento no runbook); SLOs formais (E10/E12); dashboards além dos 3 ("sem mais um painel"); Alertmanager separado (Grafana Alerting cobre o roteamento).

## Requisitos funcionais

- RF01 — Prometheus `v3.15.0` com scrape a cada 15s (retention 7d / 1GB — `--storage.tsdb.retention.time` e `.size`) dos jobs: `morfeu` (app:8080/metrics), `node`, `cadvisor`, `postgres`, `redis`, `rabbitmq` (`:15692/metrics` + `/metrics/detailed` com `queue_coarse_metrics` e `queue_consumer_count`), `prometheus`.
- RF02 — Exporters: node-exporter `v1.12.1` (`/proc`,`/sys`,`/` read-only; coletores padrão), cAdvisor `v0.60.6` (socket e `/var/lib/docker` read-only; `--docker_only`, housekeeping 30s, métricas desnecessárias desabilitadas), postgres-exporter `v0.20.1` (usuário `morfeu_monitor` com `pg_monitor`), redis_exporter `v1.92.1`.
- RF03 — Loki `3.7.8`: `auth_enabled: false`, filesystem, schema TSDB v13, `compactor.retention_enabled: true` + `delete_request_store: filesystem`, `limits_config.retention_period: 336h` (14d), `ingestion_rate_mb`/`burst` conservadores e `max_line_size`. Loki não tem retenção por tamanho nativa: o teto de disco é garantido pelos limites de ingestão + alerta de disco (RF06) — desvio consciente do "E por tamanho" do refinamento.
- RF04 — Alloy `v1.20.1`: `discovery.docker` → `discovery.relabel` (label `container` e `service` do compose; containers fora do projeto descartados) → `loki.source.docker` → `loki.process` (`stage.docker`) → `loki.write`. Sem extração de campos do JSON em labels (cardinalidade: `trace_id` fica no corpo, consultável por `| json`).
- RF05 — Grafana `13.2.3`: `GF_AUTH_ANONYMOUS_ENABLED=false`, `GF_USERS_ALLOW_SIGN_UP=false`, admin via `GF_SECURITY_ADMIN_PASSWORD` (env); datasources Prometheus (uid `prometheus`) e Loki (uid `loki`); dashboards (pasta "Morfeu"): **API — golden signals** (taxa, erro 5xx, p95 por `http_route` + logs do app), **Infra — USE** (CPU/mem/disco da VM, containers, conexões PG, memória Redis/RabbitMQ), **Mensageria** (outbox pendentes/lag, DLQ, filas prontas/consumidores).
- RF06 — Alertas Grafana-managed (pasta "Morfeu", intervalo 1m), lista fechada: (1) disco > 80%; (2) API fora (`up{job="morfeu"} == 0`); (3) taxa de 5xx > 5% em 5m (limite provisório até o SLO do E10); (4) DLQ crescendo (`increase(morfeu_dlq_mensagens[15m]) > 0` → via `delta`); (5) consumidor parado (mensagens prontas em `catalogo.filme_criado` > 0 e consumidores == 0 por 5m); (6) conexões PG > 80% de `max_connections`. Contact point Discord (`$DISCORD_WEBHOOK_URL`) + política padrão.
- RF07 — Makefile: `obs-up`, `obs-down` (compose v2, `docker compose`).

## Requisitos não funcionais

- RNF01 — Segurança (doc.md §14.5): nenhuma porta publicada além do Grafana (localhost); sem senha literal em arquivo versionado; socket do Docker montado **read-only** só em Alloy e cAdvisor; exporters com credencial de leitura mínima.
- RNF02 — Recursos (A1, 24GB compartilhados): `mem_limit` por serviço (prometheus 512m, loki 384m, grafana 256m, alloy 256m, cadvisor 192m, exporters 64m) e retenções curtas.
- RNF03 — Imagens pinadas por tag exata, todas com manifest `linux/arm64` verificado em 2026-09-29.
- RNF04 — Reconstituível: `docker compose -f docker-compose.yml -f docker-compose.observability.yml --profile app up -d` sobe tudo do zero.

## Regras de negócio

N/A — task de infraestrutura, sem regra de domínio.

## Critérios de aceite

- [ ] CA01 — Teste de integração: Prometheus sobe com a config real e `/-/ready` responde; `promtool check config` passa.
- [ ] CA02 — Teste de integração: Loki sobe com a config real (`/ready`), retenção habilitada aceita.
- [ ] CA03 — Teste de integração: Grafana com o provisioning real → API lista 3 dashboards na pasta Morfeu, datasources `prometheus`/`loki`, **6 regras de alerta** e o contact point Discord; login anônimo negado (401).
- [ ] CA04 — Teste de integração: config do Alloy válida (`alloy fmt` sem erro).
- [ ] CA05 — Verificação manual local registrada no plan.md: stack completa sobe; targets do Prometheus `up == 1`; logs do app visíveis no Loki com `trace_id`.
- [ ] CA06 — `docker compose config` do conjunto não publica portas além de `127.0.0.1:3000` (Grafana) no arquivo de observabilidade (checagem no teste por parse do YAML).
- [ ] CA07 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Prometheus real + config do repo → ready + promtool | integração (testcontainers) | CA01 |
| Loki real + config do repo → ready | integração | CA02 |
| Grafana real + provisioning do repo → API (search, datasources, alert-rules, contact-points, anônimo 401) | integração | CA03 |
| Alloy `fmt` da config | integração (container one-shot) | CA04 |
| Parse do compose de observabilidade: portas publicadas | unit | CA06 |
| Stack completa local | manual registrada | CA05 |

## Plano de implementação

1. Registrar imagens no `lib.md` (tabela de infraestrutura).
2. Compose de observabilidade + ajustes do compose base (app, plugin, init PG).
3. Configs Prometheus/Loki/Alloy.
4. Grafana: datasources, dashboards, alerting.
5. Teste de integração da stack (`test/observabilidade/`), Makefile, `.env.observability.example`, runbook.
6. Verificação manual local (CA05), lint, suíte, gate.

**Skills de apoio (§4.4):** `docker-patterns`, `golang-observability-opentelemetry`.

## Arquivos que serão criados

- `docker-compose.observability.yml`
- `configs/prometheus/prometheus.yml`
- `configs/loki/loki.yml`
- `configs/alloy/config.alloy`
- `configs/rabbitmq/enabled_plugins`
- `configs/postgres/01-usuario-monitoracao.sh`
- `configs/grafana/provisioning/datasources/datasources.yml`
- `configs/grafana/provisioning/dashboards/dashboards.yml`
- `configs/grafana/provisioning/alerting/alertas.yml`
- `configs/grafana/dashboards/api-golden-signals.json`, `infra-use.json`, `mensageria.json`
- `.env.observability.example`
- `test/observabilidade/stack_integration_test.go`
- `docs/observabilidade.md`
- `docs/prd/0007-stack-observabilidade.md`

## Arquivos que serão modificados

- `docker-compose.yml` — serviço `app` (profile), plugin RabbitMQ, init PG; remove o `version:` obsoleto (warning do compose v2).
- `.gitignore` — exceção `!.env.observability.example` (o padrão `.env.*` escondia o modelo).
- `go.mod`/`go.sum` — `gopkg.in/yaml.v3` promovido de indireto a direto (teste CA06).
- `.env.docker-compose.example` — `PG_MONITOR_PASSWORD` + URLs do app em container.
- `Makefile` — `obs-up`/`obs-down` (e `docker compose` v2 nos alvos existentes).
- `lib.md`, `docs/tasks/0006-…`/`docs/prd/0006-…` (status concluído), `docs/tasks/0007-…`, `docs/tasks/README.md`, `plan.md`, `state.md`.

Total real: 29 (≤ 30). `docs/roadmap.md` já referencia 0006+0007 — sem mudança nesta task.

**Ajustes descobertos na verificação manual (CA05):** node-exporter sem `rslave` no bind de `/` (falha em hosts cujo `/` não é mount compartilhado, ex.: WSL — as métricas de `/` não dependem disso); regex do Alloy `(?i)morfeu.*` (projetos compose `morfeu*`: dev, verificação, prod).

## Dependências utilizadas

Nenhuma dependência Go nova (o teste usa `testcontainers-go` e `gopkg.in/yaml.v3`, já no grafo — yaml.v3 hoje indireto passa a direto, registrar no lib.md). Imagens novas (tabela de infra do `lib.md`): prom/prometheus v3.15.0, grafana/grafana 13.2.3, grafana/loki 3.7.8, grafana/alloy v1.20.1, prom/node-exporter v1.12.1, ghcr.io/google/cadvisor v0.60.6, prometheuscommunity/postgres-exporter v0.20.1, oliver006/redis_exporter v1.92.1.

## Impactos técnicos

- Compose base ganha o serviço `app` opcional (profile) — `docker compose up` sem profile continua subindo só a infra.
- Volume do Postgres **existente** não roda o init: o runbook traz o comando para criar o usuário manualmente.
- CI: o job `test` passa a puxar imagens de Grafana/Prometheus/Loki/Alloy (arm64) — impacto de tempo aceitável; teste isolado em pacote próprio.

## Riscos

- Sintaxe de provisioning (alerting) errada derruba o Grafana no boot → CA03 sobe o Grafana real com os arquivos do repo.
- Memória na A1 → `mem_limit` + retenções curtas + alerta de disco.
- Socket Docker (Alloy/cAdvisor) → read-only, sem porta publicada; risco residual registrado (acesso ao socket ≈ root no host).
- Webhook do Discord ausente em dev → default para endpoint inválido documentado (alertas avaliam, envio falha em log) — nunca bloqueia o boot.

## Estratégia de rollback

Reverter o merge; a stack é opcional (arquivo separado) e o serviço `app` fica atrás de profile — o compose base sem profile se comporta como antes. Sem migration. Volumes `morfeu-prometheus`/`morfeu-loki`/`morfeu-grafana` podem ser removidos com `docker compose -f ... down -v`.
