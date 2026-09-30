# PRD 0016 — Reserva: ocupação pública, cache e alertas (E4, T2)

- **Task:** docs/tasks/0016-reserva-ocupacao.md
- **Branch:** feature/0016-reserva-ocupacao
- **Data:** 2026-09-29
- **Status:** concluído

## Objetivo

Dar ao mapa interativo do E5 a ocupação dos assentos de uma sessão. O SPA busca o layout uma vez (`/sessoes/{id}/mapa`) e consulta só a ocupação a cada 3–5 s. Um cache curto absorve esse polling, e dois alertas avisam sobre disputa anormal e sweeper parado. Fontes: `docs/refinamentos/E4-reserva.md` §T2 e ADR 0008 ("Estratégias para minimizar os trade-offs").

## Escopo

Endpoint público de ocupação, cache Redis com TTL de 3 s, 2 regras de alerta no Grafana, testes. Fecha o E4.

## Fora de escopo

UI (E5); ingressos/pedido (E6); purge de holds terminais (hardening); anti-stampede do cache (volumetria de portfólio; TTL de 3 s limita o custo de um miss).

## Requisitos funcionais

- RF01 — `GET /sessoes/{id}/ocupacao` (público, sem cookie): `200 {"sessao_id": N, "ocupados": ["A1", ...]}`, códigos em ordem crescente. Só entram holds vivos (`status='ativo' AND expires_at > agora`, relógio injetado). Nenhum dado de dono, id de hold ou prazo.
- RF02 — Sessão indisponível (inexistente, cancelada, já iniciada) → 404. A verificação usa a porta `FonteSessoes` do PRD 0015 e é feita só no miss do cache.
- RF03 — Cache read-through `reserva:ocupacao:{id}`, **TTL 3 s, sem invalidação ativa** (decisão do refinamento). O valor guarda só a lista de códigos. Se o cache falhar, a resposta vem do banco (o cache nunca derruba a rota).
- RF04 — `Cache-Control: public, max-age=2` (o navegador não segura além do TTL do servidor).
- RF05 — Alertas (Grafana, severidade `warning`):
  - "Recusas de trava anormais": `sum(increase(reserva_holds_indisponiveis_total[5m])) > 60` por 5 min (disputa real ou bot).
  - "Sweeper de holds parado": nenhuma passada (`reserva_sweeper_execucoes_total`) em 10 min com a API no ar, por 5 min.

## Requisitos não funcionais

- RNF01 — Consulta sobre o índice único parcial `holds_assento_ativo` (sem índice novo).
- RNF02 — Depguard `reserva-domain` passa a permitir `internal/cache` (interface da plataforma).
- RNF03 — Testes com PG e Redis reais.

## Regras de negócio

- RN01 — Ocupado = hold vivo. Hold vencido e ainda `ativo` (sweeper não passou) conta como livre.
- RN02 — A verdade final é a trava (409); a ocupação pode estar até 3 s defasada.

## Critérios de aceite

- [ ] CA01 — Contrato: só `sessao_id` + `ocupados`; nenhum id de hold, dono ou prazo no corpo.
- [ ] CA02 — Vencido não aparece (relógio +10 min); liberado não aparece.
- [ ] CA03 — Cache: TTL da chave no Redis em (0, 3 s]; hold criado com o cache valendo não aparece; depois que a chave expira (removida no teste), aparece.
- [ ] CA04 — Sessão cancelada/iniciada/inexistente → 404 (e nada é cacheado).
- [ ] CA05 — `Cache-Control` presente.
- [ ] CA06 — Stack de observabilidade com 9 regras (teste de integração atualizado).
- [ ] CA07 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Ocupação pelas rotas reais, PG + Redis reais, relógio injetado | integração | CA01–CA05 |
| Provisionamento do Grafana | integração (stack) | CA06 |

## Plano de implementação

1. Query `OcupadosDaSessao` + `sqlc generate`.
2. `ocupacao.go` (serviço com cache) + rota no handler.
3. Wiring do cache no `main`; depguard.
4. Alertas + teste da stack.
5. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `golang-testing`.

## Arquivos que serão criados

- `internal/reserva/ocupacao.go`
- `docs/tasks/0016-reserva-ocupacao.md`, `docs/prd/0016-reserva-ocupacao.md`

## Arquivos que serão modificados

- `internal/reserva/queries.sql`, `internal/reserva/db/queries.sql.go` (gerado), `internal/reserva/service.go` (Config.Cache), `internal/reserva/handler.go`, `internal/reserva/handler_test.go`
- `cmd/morfeu/main.go`, `.golangci.yml`
- `configs/grafana/provisioning/alerting/alertas.yml`, `test/observabilidade/stack_integration_test.go`
- `docs/tasks/README.md`, `docs/tasks/0015-reserva-trava.md`, `docs/prd/0015-reserva-trava.md` (status), `docs/roadmap.md`, `plan.md`, `state.md`

Total: 18.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- Nova rota pública de leitura; +2 alertas no Grafana.

## Riscos

- Defasagem visível ao usuário (até 3 s) → o 409 da trava é a verdade; o E5 trata o 409 atualizando o mapa.

## Estratégia de rollback

Reverter o merge (sem migration).
