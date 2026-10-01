# Observabilidade — runbook (tasks 0006/0007, 0040/0041)

Stack self-hosted do E0d: o binário expõe `/metrics` e logs JSON com `trace_id` (task 0006); o compose de observabilidade (task 0007) coleta, guarda e alerta. Tudo reconstituível por código.

## Subir / derrubar

```bash
cp .env.docker-compose.example .env.docker-compose     # e troque as senhas
cp .env.observability.example  .env.observability      # idem; DATA_SOURCE_PASS = PG_MONITOR_PASSWORD
make obs-up      # = docker compose -f docker-compose.yml -f docker-compose.observability.yml --profile app up -d --build
make obs-down
```

- Grafana: http://127.0.0.1:3000 (admin + `GF_SECURITY_ADMIN_PASSWORD`). É o **único** serviço com porta publicada pelo compose de observabilidade.
- Prometheus, Loki, Alloy e exporters só existem na rede interna. Para inspecionar: `docker exec morfeu-prometheus wget -qO- localhost:9090/api/v1/targets`.
- `docker compose up` **sem** profile continua subindo só PG/Redis/RabbitMQ (dev com `go run`).

## O que existe

| Peça | Onde | Notas |
|---|---|---|
| Prometheus v3.15.0 | `configs/prometheus/prometheus.yml` | scrape 15s; retenção 7d **e** 1GB |
| Exporters | compose de observabilidade | node, cAdvisor (`--docker_only`), postgres (usuário `pg_monitor`), redis, `rabbitmq_prometheus` (plugin nativo, `:15692`) |
| Loki 3.7.8 | `configs/loki/loki.yml` | retenção 14d (compactor) + limites de ingestão; sem retenção por tamanho nativa → teto via alerta de disco |
| Alloy v1.20.1 | `configs/alloy/config.alloy` | logs dos containers de projetos `morfeu*`; labels `container`/`service`; `trace_id` no corpo (`| json`). **Traces (0040):** receptor OTLP HTTP interno → `memory_limiter` → remoção de atributos sensíveis → **tail sampling** (erros, > 300 ms, 10% — ADR 0012) → Tempo; 512 MB |
| Tempo 2.10.4 | `configs/tempo/tempo.yml` | monolítico, disco local (volume `morfeu-tempo`), retenção 72 h, sem porta publicada |
| Grafana 13.2.3 | `configs/grafana/` | datasources `prometheus`/`loki`/`tempo` (log ↔ trace pelo `trace_id`); 7 dashboards (pasta Morfeu); 18 alertas → Discord + heartbeat externo |

Dashboards (lista fechada — "sem mais um painel" sem PRD): **API — golden signals**, **Infra — USE**, **Mensageria — outbox e DLQ** e, de negócio (0041), **funil do checkout**, **compensações e cancelamentos**, **reservas e ocupação**, **gateway e e-mail**.

## Traces (task 0040)

- O app só exporta com `OTEL_EXPORTER_OTLP_ENDPOINT` (o compose de observabilidade injeta `http://alloy:4318`); sem ele, amostragem de cabeça 10% + descarte.
- No Grafana: Explore → Tempo → TraceQL, ex.: `{ status = error }` ou `{ resource.service.name = "morfeu" && duration > 300ms }`. Num log do Loki, o campo `trace_id` vira link "Ver trace".
- Não vão aos spans: query string, URL completa, headers, parâmetros de query, IP e user agent (app e Alloy). O path do ingresso sai como rota (`/i/:ref`).
- Limitação: partes assíncronas da saga que cheguem mais de 10 s depois do primeiro span ficam fora da decisão do tail sampling.

## Alertas (lista fechada)

| Alerta | Condição | Severidade |
|---|---|---|
| Disco acima de 80% | `/` da VM > 80% por 5m | critical |
| API fora do ar | `up{job="morfeu"}` ≠ 1 por 2m | critical |
| Taxa de erro 5xx acima de 5% | 5m (provisório até o SLO do E10) | warning |
| DLQ crescendo | `delta(morfeu_dlq_mensagens[15m]) > 0` | warning |
| Consumidor parado | prontas em `catalogo.filme_criado` > 0 **e** 0 consumidores por 5m | critical |
| Conexões do PostgreSQL acima de 80% | `pg_stat_activity_count / max_connections` por 5m | warning |
| Reuso de refresh token detectado (E1, task 0010) | `increase(auth_refresh_reuso_total[15m]) > 0` — **triagem:** até o single-flight do SPA (E8), logout numa aba seguido de refresh em outra também dispara | critical |

| … (E4/E6/E7) | reserva, saga e e-mail — ver `configs/grafana/provisioning/alerting/alertas.yml` | — |
| Purga da trilha de auditoria parada (0041) | `time() - max(auditoria_purga_ultima_execucao_timestamp) > 48h`; sem série = OK (processo sem worker) — **ação:** log "purga da trilha de auditoria" no worker | warning |
| Watchdog (0041) | `vector(1)` — **sempre disparando**; não vai ao Discord: pinga `HEALTHCHECKS_PING_URL` a cada 5 min. **Não silenciar** | none |

A lista fechada (18 uids) é conferida por nome em `test/observabilidade/stack_integration_test.go`.

**Watchdog (heartbeat externo — refinamento E10):** crie um check no healthchecks.io (período 5 min, graça ~10 min, notificação por e-mail/Discord) e ponha a URL de ping em `HEALTHCHECKS_PING_URL` no `.env.observability` (é segredo: quem a conhece forja o heartbeat). Se o ping parar — VM, Grafana ou a cadeia de alertas mortos —, quem avisa é o serviço externo. Sem conta, mantenha o placeholder do `.env.observability.example`. O monitor HTTP da API pública fica para a E0c-CD (precisa de URL pública).

Destino: contact point `discord-morfeu` (`DISCORD_WEBHOOK_URL`). Em dev use o placeholder do `.env.observability.example` — o contact point exige URL válida no boot; o envio falha em log sem bloquear nada.

## Consultas úteis

- Logs de um trace: `{service="app"} | json | trace_id="<id>"`
- Erros do app: `{service="app"} | json | level=~"warn|error"`
- Consumo de mensagens: `{service="app"} |= "mensagem consumida"`

## Volume do Postgres já existente

O usuário de monitoração é criado pelo init (`configs/postgres/01-usuario-monitoracao.sh`) **só em volume novo**. Em volume existente, rode uma vez:

```bash
docker exec -it morfeu-postgres psql -U postgres -d morfeu \
  -c "CREATE ROLE morfeu_monitor LOGIN PASSWORD '<PG_MONITOR_PASSWORD>'" \
  -c "GRANT pg_monitor TO morfeu_monitor"
```

## Riscos aceitos e pendências

- Alloy e cAdvisor montam o socket do Docker **read-only** — acesso ao socket ≈ root no host. Nenhum dos dois publica porta.
- `/metrics` do app está na porta do app: **fechar antes do primeiro deploy** (E0c-CD — Caddy não roteia `/metrics`, porta do app só na rede interna do compose prod).
- Heartbeat externo entregue (0041); **monitor HTTP da URL pública** e aceite manual do watchdog (desligar o Grafana e ver o aviso chegar) ficam para a E0c-CD.

## Validação automatizada

`test/observabilidade/stack_integration_test.go` (tag `integration`) sobe Prometheus (`promtool check config`), Loki (`/ready`), Alloy (`alloy validate`), Alloy + Tempo (trace com erro e trace lento chegam pelo tail sampling) e Grafana reais com as configs do repositório e verifica 7 dashboards, 3 datasources, as 18 regras por uid, os 2 contact points e o 401 anônimo; também garante que o compose de observabilidade só publica o Grafana em 127.0.0.1. `contrato_test.go` garante que dashboards e alertas só citam métricas que o app declara.
