# PRD 0046 — Execução local da carga e relatório do M5 indicativo (E12, T2)

- **Task:** docs/tasks/0046-execucao-carga.md
- **Branch:** feature/0046-execucao-carga
- **Data:** 2026-10-01
- **Status:** concluída (auditoria APROVADA em 2026-10-02; PR #81)

## Objetivo

Rodar os cenários do ferramental (0045) nesta máquina e registrar o **M5 indicativo**, que fecha o E12 e o roadmap (decisão do usuário no refinamento E12). O M5 oficial, na VM, vira aceite da E0c-CD. Fonte: `docs/refinamentos/E12-teste-de-carga.md` §T2.

## Escopo

Execuções, relatório versionado, ajustes pequenos que a carga exigir (com teste), registro do M5 oficial no checklist da E0c-CD e fechamento do roadmap.

## Fora de escopo

- M5 oficial, soak longo, recalibração do Argon2 e RSS realista do Alloy (VM — E0c-CD).
- Mudança estrutural (worker separado, troca da trava, etc.): vira ADR/task nova se a evidência pedir.

## Requisitos funcionais

- RF01 — Execuções (stack `deploy/carga` + observabilidade, seed, `ANALYZE`), cada uma seguida do `invariante.sh`:
  1. Baseline leitura 10 req/s × 3 min com cache quente, e de novo com cache frio (`FLUSHALL` no Redis antes).
  2. Ramp de leitura 50 → 100 → 200 → 300 → 400 req/s (patamares de 3 min) — onde fica o joelho.
  3. **SLO**: leitura 300 req/s × 10 min, **3 vezes**; veredito pela mediana.
  4. Misto 90/10 (270 leitura + 30 escrita) × 10 min.
  5. Disputa (N travas paralelas no mesmo assento) — no máximo 1 vencedor por assento, zero 5xx.
  6. Checkout fim a fim (trava → pedido → `/__teste/pagar` → ingresso) — SLI, alvo definido aqui.
  7. Soak misto 30 min (memória do app, goroutines, pool do PG, filas do RabbitMQ, RSS do Alloy).
- RF02 — Validade de cada execução: CPU do gerador ≤ ~70% e `dropped_iterations = 0`; fora disso a execução é **inválida** e é refeita (com taxa menor, se o gerador for o limite — e isso é registrado).
- RF03 — Veredito server-side pelas queries de `docs/carga/queries.md` (janela do patamar, descartando 60 s), comparado com o client-side do k6.
- RF04 — Relatório `docs/carga/2026-10-01-local.md`: selo **"indicativo (local)"** no topo; ambiente (CPU, RAM, `.wslconfig`, Docker, commit, digest do k6, limites do compose); dataset; tabela por execução (req/s alcançado, p50/p95/p99 server e client, erro, dropped, CPU do gerador); disputa e checkout; soak; alertas que dispararam; gargalos e decisões; o que fica para a VM.
- RF05 — Ajustes pequenos que a carga exigir entram com teste de regressão (ex.: single-flight do cartaz **só** se o cache frio mostrar stampede — `golang.org/x/sync/singleflight` no `lib.md` + teste de concorrência com `-race`).
- RF06 — `docs/deploy-checklist.md` ganha o **M5 oficial** (mesmos scripts e queries, gerador fora da VM) e a recalibração do Argon2; `docs/observabilidade.md` corrige "19 → 20 alertas" (pendência da 0045); roadmap marca E12 ✅ e M5 "indicativo ✅ / oficial no aceite da E0c-CD".

## Requisitos não funcionais

- RNF01 — Relatório sem segredos, hosts internos sensíveis ou PII (dataset sintético).
- RNF02 — Números locais nunca apresentados como oficiais.

## Critérios de aceite

- [x] CA01 — Todas as execuções do RF01 válidas (ou inválidas com motivo registrado e refeitas), invariante verde em todas.
- [x] CA02 — Veredito do M5 indicativo explícito: p95 server-side das leituras a 300 req/s (mediana de 3) e erro, contra o SLO (< 300 ms, < 1%).
- [x] CA03 — Disputa sem assento vendido duas vezes e sem 5xx; checkout com SLI medido.
- [x] CA04 — Relatório completo (RF04) versionado; ajustes (se houver) com teste.
- [x] CA05 — Checklist da E0c-CD, `docs/observabilidade.md`, roadmap, `state.md` e `plan.md` atualizados.
- [ ] CA06 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Execuções de carga + invariante | manual (k6 + `invariante.sh`) | CA01–CA03 |
| Ajustes de performance (se houver) | unitário/integração com `-race` | CA04 |

## Plano de implementação

1. Subir a stack, seed, conferir métricas no Prometheus; 2. baseline e ramp; 3. SLO 3×; 4. misto, disputa, checkout; 5. soak; 6. ajustes (se houver) e reexecução do que mudou; 7. relatório e docs.

**Skills de apoio (§4.4):** `golang-observability-opentelemetry`, `golang-concurrency` (só se houver ajuste).

## Arquivos que serão criados

- `docs/carga/2026-10-01-local.md`, `docs/tasks/0046-execucao-carga.md`, `docs/prd/0046-execucao-carga.md`

## Arquivos que serão modificados

- `docs/deploy-checklist.md`, `docs/observabilidade.md`, `docs/carga/runbook.md` (se a execução revelar passo faltando), `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`
- Ajustes que a execução exigiu (registrados ao surgir — `plan.md`, checkpoint de desvio):
  - `internal/notificacao/email.go` + `internal/notificacao/consumidor_test.go` — e-mail fake com histórico limitado (RSS crescente no soak)
  - `deploy/carga/k6/lib.js` (`SESSOES_PULAR`) + `deploy/carga/k6/disputa.js` (threshold `disputa_vencedores > 0`) — disputa vazia passava verde
  - `deploy/carga/invariante.sh`, `deploy/carga/smoke.sh` — modo `100755`
  - `docs/carga/queries.md` — CPU do gerador com `[2m]`; alertas pela API do Grafana

Total estimado: 10 (+ ajustes). Real: 18.

## Dependências utilizadas

Nenhuma nova (só `singleflight` se a evidência pedir — registrar antes).

## Impactos técnicos

Nenhum no app sem ajuste; ~3 h de relógio de execução nesta máquina.

## Riscos

- Gerador saturando o notebook (execução inválida) → taxa menor registrada como limite do ambiente.
- Ler números locais como oficiais → selo e seção "o que fica para a VM".

## Estratégia de rollback

Reverter o merge (só documentação, salvo ajustes com teste).
