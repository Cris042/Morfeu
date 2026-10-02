# Carga — PromQL do veredito (E12, task 0045)

O **veredito do SLO é server-side**: sai do Prometheus do app, não do k6. O k6 só diz se o gerador sustentou a taxa (guarda-corpo). Estas queries são **a fonte única** do relatório da 0046 e do M5 oficial da E0c-CD — copiar daqui, não reescrever.

## Janela

- O **patamar** é o trecho de taxa constante do cenário (ex.: os 10 min a 300 req/s). **Descarta-se o 1º minuto (60 s)**: aquecimento de cache/pool/JIT de conexões.
- `$FIM` = fim do patamar (UTC); `$JANELA` = duração do patamar − 60 s (ex.: patamar de 10 min → `9m`). Todas as queries são **instantâneas avaliadas em `$FIM`** (`/api/v1/query?query=…&time=$FIM`), então `[$JANELA]` cobre exatamente `[início+60 s, fim]`.
- Registre no relatório `$FIM` e `$JANELA` em UTC. Três repetições do patamar do SLO: use a **mediana** dos três valores.
- Fronteiras do histograma HTTP (s): `0,005 0,01 0,025 0,05 0,1 0,2 0,3 0,5 1 2,5 5 10` — a fronteira de **0,3 s** é a do SLO, então o p95 não é interpolado em volta dela.

Labels: `job="morfeu"`, `http_route` (rota **templada**, nunca o path bruto), `http_response_status_code`. `/health` e `/metrics` não entram.

## 1. Latência p95 por rota (SLO: p95 < 300 ms)

Rotas do SLO: `/filmes` (cartaz), `/filmes/:id/sessoes` (sessões), `/sessoes/:id/ocupacao` (ocupação).

```promql
# uma por vez — troque o valor de http_route
histogram_quantile(0.95,
  sum by (le) (rate(http_server_request_duration_seconds_bucket{job="morfeu", http_route="/sessoes/:id/ocupacao"}[$JANELA])))
```

Todas as rotas de uma vez (mesma janela):

```promql
histogram_quantile(0.95,
  sum by (le, http_route) (rate(http_server_request_duration_seconds_bucket{job="morfeu"}[$JANELA])))
```

p50 e p99: troque `0.95` por `0.5` / `0.99`. A porcentagem de requisições dentro do SLO (≤ 0,3 s) por rota:

```promql
sum by (http_route) (rate(http_server_request_duration_seconds_bucket{job="morfeu", le="0.3"}[$JANELA]))
/
sum by (http_route) (rate(http_server_request_duration_seconds_count{job="morfeu"}[$JANELA]))
```

## 2. Erro (SLO: < 1%) — 5xx sobre o total, **sem contar 409**

409 é resposta de negócio esperada na disputa de assento (`assento_indisponivel`), não falha. Fica fora do numerador (já não é 5xx) e **do denominador**:

```promql
sum(rate(http_server_request_duration_seconds_count{job="morfeu", http_response_status_code=~"5.."}[$JANELA]))
/
sum(rate(http_server_request_duration_seconds_count{job="morfeu", http_response_status_code!="409"}[$JANELA]))
```

Por classe de status (diagnóstico — 429 indicaria limite por IP ativo, ou seja, `MORFEU_LOADTEST` desligado):

```promql
sum by (http_response_status_code) (rate(http_server_request_duration_seconds_count{job="morfeu"}[$JANELA]))
```

## 3. Vazão alcançada (req/s)

```promql
sum(rate(http_server_request_duration_seconds_count{job="morfeu"}[$JANELA]))
sum by (http_route) (rate(http_server_request_duration_seconds_count{job="morfeu"}[$JANELA]))
```

Compare com a taxa pedida ao k6: vazão < taxa pedida = servidor saturado **ou** gerador limitado (conferir `dropped_iterations` e a CPU do gerador, §5).

## 4. Recursos (requer a stack de observabilidade combinada — runbook)

```promql
# RSS do Alloy (pico no patamar) e do app — cAdvisor
max_over_time(container_memory_rss{name="morfeu-alloy"}[$JANELA])
max_over_time(container_memory_rss{name="morfeu-carga-app"}[$JANELA])

# CPU do app (em núcleos; limite do compose = 2)
rate(container_cpu_usage_seconds_total{name="morfeu-carga-app"}[$JANELA])

# Pool/conexões do PG — postgres-exporter (o app não expõe o pool do pgx)
max_over_time(sum(pg_stat_activity_count{datname="morfeu_carga"})[$JANELA:15s])
max_over_time((sum(pg_stat_activity_count{datname="morfeu_carga", state="active"}))[$JANELA:15s])
max(pg_settings_max_connections)
```

O pool do app tem teto `POOL_MAX_SIZE` (25 por padrão): `pg_stat_activity_count` ≈ teto durante o patamar = pool saturado.

## 5. Validade do run (gerador)

Run **inválido** (não "provisório") se qualquer uma:

- CPU do gerador > ~70% do limite dele (2 núcleos no compose): 

```promql
max(max_over_time((rate(container_cpu_usage_seconds_total{name=~".*k6.*"}[2m]) / 2)[$JANELA:30s])) > 0.7
```

  Janela do `rate` de **2 min**, não menos: o cAdvisor é raspado a cada 30 s e atualiza as amostras de forma esparsa — com `[30s]`/`[1m]` a série do k6 volta vazia (verificado na 0046). Conferência na hora: `docker stats` (lá o limite é 200%, não 100%).

- `dropped_iterations` > 0 no resumo do k6 (o threshold do script já falha o run);
- `morfeu_modo_loadtest` = 0 (limites por IP voltaram a valer e o run mede o rate limit, não a carga).

## 6. Alertas durante o run

Os alertas são **do Grafana** (unified alerting), não regras do Prometheus — a série `ALERTS` **não existe** no Prometheus. Consulte o estado no Grafana (Alerting → regras) ou pela API, com o admin do `.env.observability`:

```bash
curl -s -u "$GF_SECURITY_ADMIN_USER:$GF_SECURITY_ADMIN_PASSWORD" \
  http://127.0.0.1:3000/api/prometheus/grafana/api/v1/alerts      # state = Alerting | Normal | Pending
```

Esperados durante um run: **"Modo de carga (MORFEU_LOADTEST) ligado"** e o **Watchdog** (sempre). Registre no relatório qualquer outro.
