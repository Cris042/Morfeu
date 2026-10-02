# PRD 0045 — Ferramental de carga (E12, T1)

- **Task:** docs/tasks/0045-ferramental-carga.md
- **Branch:** feature/0045-ferramental-carga
- **Data:** 2026-10-01
- **Status:** concluído

## Objetivo

Tudo que a execução do M5 precisa, sem abrir brecha em produção: modo de carga travado por guardas, histograma com fronteira no SLO, dataset sintético, cenários k6, invariante de assento e as queries do veredito. Fonte: `docs/refinamentos/E12-teste-de-carga.md` §T1.

## Escopo

Flag `MORFEU_LOADTEST` (config, limites por IP, gauge, alerta, rota de teste), View de buckets, `deploy/carga/` (compose, seed, invariante, scripts k6, smoke), `docs/carga/` (queries e runbook), `lib.md`.

## Fora de escopo

- Executar a carga e o relatório (0046).
- `workflow_dispatch` e alvo remoto (adiado até a VM — refinamento E12).
- Single-flight do cartaz, worker separado, recalibrar Argon2 (só com evidência — 0046/VM).

## Requisitos funcionais

- RF01 — `MORFEU_LOADTEST=1` → `Config.LoadTest`. `Validate` recusa a flag com `AMBIENTE=producao`, `MORFEU_GATEWAY` ≠ `fake`, `EMAIL_PROVEDOR` ≠ `fake` ou banco (nome em `DATABASE_URL`) que não termina em `_carga`. Sem a flag, nada muda.
- RF02 — `limiteEfetivo(base int, loadtest bool) int` (×1000 com a flag) aplicado **só** aos limites por IP (travas, pedidos, consulta, ingresso, login/registro por IP). Por dono, por conta/e-mail e o do webhook ficam iguais.
- RF03 — Com a flag: log WARN no boot; gauge `morfeu_modo_loadtest` = 1 (0 sem a flag); alerta `morfeu-modo-loadtest` (dispara com valor 1; regra 20, conferida por uid no teste da stack).
- RF04 — `POST /__teste/pagar/:id` (e `/__teste/emails`) só registrada com o fake **e** `AMBIENTE != producao`.
- RF05 — View do instrumento `http.server.request.duration` (otelecho v0.70.0) com fronteiras `0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.3, 0.5, 1, 2.5, 5, 10`, repetindo o filtro de atributos `LabelsPermitidos` (sem stream duplicado).
- RF06 — `deploy/carga/docker-compose.carga.yml`: stack local (PG com banco `morfeu_carga`, Redis, RabbitMQ, app com a flag, fakes, `GOMEMLIMIT`, `cpus`/`mem_limit` próximos da VM — 2 vCPU/2 GB para o app; observabilidade pelo compose existente) e o serviço `k6` (`grafana/k6:2.3.0@sha256:9c2dee7f8ed74d317e4027c06a10f169b625638189de8d4555d0b3486a5aeb34`, `cpus` próprio, profile `k6`), sem portas além de `127.0.0.1`.
- RF07 — `deploy/carga/seed.sql` (psql, parâmetros `-v`): determinístico e idempotente; recusa banco cujo nome não termina em `_carga`; filmes, salas e sessões futuras suficientes para não esgotar assentos (pelo menos 10× os assentos que a disputa e o checkout consomem); usuários `carga{n}@example.test` com um hash Argon2id literal pré-computado (a senha não é versionada — só o hash; senha documentada como descartável no runbook só se for necessária para login); `ANALYZE` ao fim.
- RF08 — `deploy/carga/invariante.sql` + `deploy/carga/invariante.sh`: zero linhas para (a) assento com mais de um hold `ativo|convertido`, (b) assento com mais de um ingresso `ativo`, (c) pedido `pago` sem ingresso; saída ≠ 0 e lista das violações se houver.
- RF09 — Scripts k6 em `deploy/carga/k6/` (`lib.js` com base URL por env e recusa de host que não seja local/rede do compose; `leitura.js` — cartaz/sessões/ocupação; `rampa.js`; `misto.js` 90/10; `disputa.js` — N VUs no mesmo assento, 409 esperado; `checkout.js` — trava → pedido → `/__teste/pagar` → ingresso; `soak.js`): open model (`constant-arrival-rate`/`ramping-arrival-rate`), `discardResponseBodies` onde couber, `setResponseCallback(expectedStatuses(...))`, thresholds de guarda-corpo (`http_req_failed`, `dropped_iterations` = 0, checks) — o veredito do SLO é server-side.
- RF10 — `deploy/carga/smoke.sh`: sobe a stack, roda o seed, `k6 inspect` em todos os scripts e cada cenário a taxa mínima por ~20 s total, depois o invariante; não é gate de PR.
- RF11 — `docs/carga/queries.md` (PromQL do p95 por rota templada para cartaz/sessões/ocupação, erro = 5xx sobre total sem contar 409, req/s, RSS do Alloy e do app, pool do PG; janela = patamar descartando 60 s) e `docs/carga/runbook.md` (como rodar cada cenário, critérios de validade — gerador ≤ ~70% de CPU e `dropped_iterations = 0` —, limpeza = recriar a stack, nunca contra URL pública, como gerar outro hash do seed).

## Requisitos não funcionais

- RNF01 — Nenhum caminho liga o modo de carga em produção (testes de boot); nenhum segredo versionado (hash Argon2 não é segredo; senha descartável fora do repo).
- RNF02 — Sem dependência Go nova; imagem k6 por versão + digest no `lib.md`.

## Critérios de aceite

- [x] CA01 — Testes de boot (table-driven): flag + produção, flag + gateway stripe, flag + e-mail resend, flag + banco sem `_carga` → erro; flag com tudo certo → ok; sem flag → nada muda.
- [x] CA02 — `limiteEfetivo` testado; com a flag os limites por IP sobem e os por dono continuam (teste de que o limite por dono das travas segue barrando).
- [x] CA03 — Rota `/__teste/pagar` ausente (404) com `AMBIENTE=producao` (teste).
- [x] CA04 — Registry expõe `http_server_request_duration_seconds_bucket{le="0.3"}`, sem série duplicada; contrato/dashboards existentes verdes.
- [x] CA05 — Alerta `morfeu-modo-loadtest` provisionado (20 regras por uid); contrato reconhece `morfeu_modo_loadtest`.
- [x] CA06 — `smoke.sh` roda limpo nesta máquina (todos os cenários a taxa mínima, checks 100%, invariante sem linhas); seed rodado 2× não duplica; seed recusa banco sem `_carga`.
- [x] CA07 — `invariante.sh` sai ≠ 0 diante de uma violação plantada (teste de integração ou passo do smoke).
- [x] CA08 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Guardas da flag | unitário (`internal/config`) | CA01 |
| `limiteEfetivo` e rota de teste em produção | unitário (`cmd/morfeu`) | CA02, CA03 |
| Buckets no registry | unitário/integração (`internal/telemetria`) | CA04 |
| Alerta e contrato | `test/observabilidade` | CA05 |
| Smoke, seed e invariante | `deploy/carga/smoke.sh` (manual) + teste de integração do invariante (`test/carga`) | CA06, CA07 |

## Plano de implementação

1. Config + `limiteEfetivo` + gauge + rota de teste; 2. View de buckets; 3. alerta; 4. compose de carga + seed + invariante; 5. scripts k6 + smoke; 6. docs + `lib.md`; 7. smoke real nesta máquina.

**Skills de apoio (§4.4):** `golang-testing`, `golang-observability-opentelemetry`.

## Arquivos que serão criados

- `deploy/carga/docker-compose.carga.yml`, `deploy/carga/seed.sql`, `deploy/carga/invariante.sql`, `deploy/carga/invariante.sh`, `deploy/carga/smoke.sh`
- `deploy/carga/k6/{lib,leitura,misto,disputa,checkout}.js` (rampa = `PERFIL=rampa` em `leitura.js`; soak = `misto.js` com `DURACAO=30m`)
- `.dockerignore` (contexto do build do app sem `.env` nem `web/node_modules` — ~191 MB a menos)
- `test/carga/invariante_integration_test.go`, `docs/carga/queries.md`, `docs/carga/runbook.md`
- `docs/tasks/0045-ferramental-carga.md`, `docs/prd/0045-ferramental-carga.md`

## Arquivos que serão modificados

- `internal/config/config.go`, `internal/config/config_test.go`, `cmd/morfeu/main.go`, `cmd/morfeu/main_test.go` (ou teste novo no pacote), `internal/telemetria/telemetria.go`, `internal/telemetria/telemetria_test.go`
- `configs/grafana/provisioning/alerting/alertas.yml`, `test/observabilidade/stack_integration_test.go`, `test/observabilidade/contrato_test.go`
- `lib.md`, `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total: 30 (no teto).

## Dependências utilizadas

Nova (ferramenta, não Go): `grafana/k6` 2.3.0 por digest (Context7 `/websites/grafana_k6`: `constant-arrival-rate`, `setResponseCallback`/`expectedStatuses` iguais na v2; a v2 removeu só o executor `externally-controlled`, não usado).

## Impactos técnicos

- Séries do histograma HTTP mudam de fronteiras (o `le="0.25"` deixa de existir) — conferir dashboards/alertas que citam `le`.
- `config.Validate` ganha uma regra; ambiente atual sem a flag não muda.

## Riscos

- Modo de carga em produção → 4 guardas no boot + gauge/alerta.
- Teto de arquivos → consolidar scripts k6 por cenário via env.

## Desvios registrados na implementação

- 5 scripts k6 em vez de 7 (rampa e soak por env).
- Disputa: cada iteração dispara N travas paralelas (`http.batch`) no mesmo assento e confere "no máximo 1 vencedor e zero 5xx" por iteração.
- View custom única (`viewPadrao`) em vez de duas Views: com duas, o SDK duplicaria o stream do HTTP; a View custom copia nome/descrição/unidade (sem isso o scrape falhava com "empty name").
- CA03 coberto pelo predicado `rotasDeTesteAtivas` (o registro real exige um `Servico` completo).
- CA02: limite por dono provado com `Limitador` real caindo no fallback em memória.
- `.dockerignore` novo (fora da lista original). O "19 → 20 alertas" do `docs/observabilidade.md` fica para a 0046 (teto de 30 arquivos).
- `postgres-exporter` aponta para `morfeu` por padrão — o runbook manda ajustar o `DATA_SOURCE_URI` para `morfeu_carga`.

## Estratégia de rollback

Reverter o merge (flag opcional; View só muda fronteiras).
