# PRD 0037 — Trilha de auditoria + consulta e cancelamento de pedido pelo operador (E9, T2)

- **Task:** docs/tasks/0037-auditoria-operador.md
- **Branch:** feature/0037-auditoria-operador
- **Data:** 2026-10-01
- **Status:** em andamento

## Objetivo

Fontes: `docs/refinamentos/E9-backoffice.md` §T2 (decisão do usuário: cancelamento individual pelo operador incluído), `doc.md` §10 (trilha enxuta, só IDs, 12 meses), ADR 0011 (motivo `operador`), ADR 0003 (fronteiras).

## Escopo

Trilha de auditoria, consulta e cancelamento de pedido pelo operador, RBAC testado em todas as rotas do backoffice. Só backend.

## Fora de escopo

- Telas do backoffice (0038).
- API de leitura da trilha (sem requisito; consulta direta no banco).

## Requisitos funcionais

- RF01 — Migration 017: `eventos_auditoria (id, ator_id uuid, acao enum, alvo_tipo enum, alvo_id numérico|UUID, ocorrido_em)`; CHECKs impedem texto livre (nenhuma PII cabe); índices `(ocorrido_em)` e `(alvo_tipo, alvo_id, ocorrido_em)`; trigger barra UPDATE e TRUNCATE.
- RF02 — Pacote de plataforma `internal/auditoria` (como o `outbox`): `Registrar(ctx, tx, acao, alvoID, agora)` na TX do chamador (erro = rollback da ação); ator lido do context (`ComAtor`), `uuid.Nil` = sistema (seed/CLI/jobs).
- RF03 — Trilha nas mutações do operador: filme criado/atualizado/arquivado/importado; sala criada/atualizada; sessão criada/cancelada (cancelamento repetido não audita de novo); pedido cancelado. As mutações de sala/sessão/filme sem TX passam a abrir uma.
- RF04 — O main liga o ator: `comAtorDaTrilha(Exigir(operador))` em todas as rotas `/backoffice/*`.
- RF05 — Purga diária no worker (`DELETE` em lotes de 5000 de eventos com mais de 12 meses); métricas `auditoria_purga_removidos_total` e `auditoria_purga_ultima_execucao_timestamp`; alerta "purga parada há mais de 48 h" (lista fechada de alertas: 16 → 17).
- RF06 — `GET /backoffice/pedidos?sessao_id=&status=&pagina=`: filtros validados (400 com o campo), 50 por página, mais recentes primeiro, e-mail **mascarado** (`a***@dominio`), **sem** o código; `no-store`.
- RF07 — `GET /backoffice/pedidos/:id`: pedido (mascarado, sem código, com o motivo do estorno), ingressos (assento + status, sem token) e trilha de estados; inexistente → 404.
- RF08 — `POST /backoffice/pedidos/:id/cancelar`: sem a janela do cliente; só `pago` sem ingresso usado; sessão já iniciada → `409 sessao_iniciada`; CAS → `estorno_pendente` (motivo `operador`) + ingressos cancelados + trilha na mesma TX; repetir → 200 sem nova trilha; métrica `cancelamentos_total{origem="operador"}`.
- RF09 — Teste de RBAC sobre **todas** as rotas `/backoffice/*` registradas pelo main: sem token → 401; cliente → 403.
- RF10 — Ajuste achado na revisão dos alertas: o job de estorno só conta `saga_compensacoes_total{passo="estorno"}` para estornos automáticos (divergência, tardio, emissão) — cancelamento não é falha do checkout e não pode disparar o alerta "Estorno automático executado".

## Requisitos não funcionais

- RNF01 — Nenhuma PII na trilha (garantido pelo schema) nem nos logs (só IDs).
- RNF02 — Depguard: `internal/auditoria` liberado para `catalogo`, `sessao` e `pedido` (plataforma).

## Regras de negócio

- RN01 — Retenção da trilha: 12 meses.
- RN02 — Append-only com um único usuário de banco: UPDATE/TRUNCATE barrados por trigger; DELETE só pela purga (limite aceito no refinamento — sem SECURITY DEFINER).

## Critérios de aceite

- [ ] CA01 — Registrar com ator do context e sem ator (sistema); ação fora do enum falha.
- [ ] CA02 — Alvo com e-mail, código ou texto livre recusado pelo banco.
- [ ] CA03 — UPDATE e TRUNCATE na trilha falham; o evento permanece.
- [ ] CA04 — Purga remove só o que passou da retenção, em lotes, e é idempotente.
- [ ] CA05 — Filme (criar/editar/arquivar) e sessão (criar/cancelar ×2) deixam a trilha esperada.
- [ ] CA06 — Consulta do operador: filtros, 400 nos inválidos, e-mail mascarado, sem código; detalhe com ingressos e trilha; 404.
- [ ] CA07 — Operador cancela dentro das 2h finais → `estorno_pendente`/`operador`, ingressos cancelados, 1 linha na trilha com o ator do token; repetir não audita nem conta; sessão iniciada → 409; cliente → 403; sem token → 401.
- [ ] CA08 — Toda rota `/backoffice/*` do main recusa sem token (401) e cliente (403); o teste falha se encontrar menos de 15 rotas.
- [ ] CA09 — Cancelamento estornado não incrementa a compensação da saga; lint, suíte `-race` e CI verdes; 17 alertas provisionados.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Registrar, PII, append-only, purga (PG real) | integração (`auditoria`) | CA01–CA04 |
| Trilha das mutações | integração (`catalogo`, `sessao`) | CA05 |
| Consulta e cancelamento do operador | integração (`pedido`) | CA06, CA07 |
| RBAC das rotas do main | unit (`cmd/morfeu`) | CA08 |
| Compensação × cancelamento; 17 alertas | integração (`pedido`, `test/observabilidade`) | CA09 |

## Plano de implementação

1. Migration 017 + `internal/auditoria` + testes.
2. Trilha nas mutações (catálogo, sessão) + migration nas suítes.
3. `pedido/operador.go` + rotas + testes.
4. Main (ator, rotas, purga, métricas), alerta, teste de RBAC.

**Skills de apoio (§4.4):** `golang-database`, `security-review`.

## Arquivos que serão criados

- `migrations/017_eventos_auditoria.{up,down}.sql`
- `internal/auditoria/auditoria.go`, `internal/auditoria/auditoria_test.go`
- `internal/pedido/operador.go`, `internal/pedido/operador_test.go`, `cmd/morfeu/backoffice_test.go`
- `docs/tasks/0037-auditoria-operador.md`, `docs/prd/0037-auditoria-operador.md`

## Arquivos que serão modificados

- `internal/catalogo/{service.go, importacao.go, handler_test.go}`
- `internal/sessao/{service.go, handler_test.go}`
- `internal/pedido/{estados.go, errors.go, handler.go, queries.sql, tarefas.go, handler_test.go, cancelamento_test.go}`, `internal/pedido/db/queries.sql.go` (gerado)
- `internal/outbox/relay_integration_test.go`, `cmd/morfeu/{main.go, main_test.go}`, `test/observabilidade/stack_integration_test.go`
- `configs/grafana/provisioning/alerting/alertas.yml`, `.golangci.yml`
- `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 30 (29 autorais + 1 gerado).

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- Mutações de sala/sessão/filme passam a abrir TX (uma escrita a mais por ação; volume de backoffice desprezível).
- Suítes que criam filme/sala/sessão precisam da migration 017.

## Riscos

- Operador comprometido cancela pedidos → trilha com o ator + RBAC testado.

## Estratégia de rollback

Reverter o merge e aplicar o down da 017 (descarta a trilha).
