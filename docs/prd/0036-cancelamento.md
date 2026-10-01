# PRD 0036 — Backend: cancelamento pelo cliente e cancelamento de sessão com vendidos (E9, T1)

- **Task:** docs/tasks/0036-cancelamento.md
- **Branch:** feature/0036-cancelamento
- **Data:** 2026-10-01
- **Status:** concluído

## Objetivo

Materializar o ADR 0011: cancelar é entrar na compensação existente. Fontes: `docs/refinamentos/E9-backoffice.md` §T1, `doc.md` §2 (cliente cancela até 2h antes), ADR 0010 (estorno, CAS, porta transacional), ADR 0003 (portas no main).

## Escopo

Domínio, migration, serviço, rotas, porta `sessao → pedido`, pivô, métrica e testes. Só backend.

## Fora de escopo

- Botão "Cancelar" na SPA e E2E do cancelamento → 0038.
- Trilha de auditoria e cancelamento individual pelo operador → 0037.
- Mudanças no job de estorno (o ADR 0011 o mantém intacto).

## Requisitos funcionais

- RF01 — Migration 016: CHECK de `pedidos.motivo_estorno` passa a aceitar `cancelamento`, `operador` e `sessao_cancelada` (além de `divergencia`, `tardio`, `emissao`); down restaura o anterior.
- RF02 — Máquina de estados: evento `CancelamentoSolicitado` com `pago → estorno_pendente`; nenhum outro estado aceita o evento.
- RF03 — Regra do cliente (domínio, função pura com relógio): cancelável se o pedido está `pago`, nenhum ingresso está `usado` e `agora ≤ inicio_sessao − 2h` (fronteira **inclusiva**).
- RF04 — Cancelamento (serviço), numa TX: trava o pedido (`FOR UPDATE`) → se já `estorno_pendente`/`estornado`, devolve o estado atual sem mudar nada (idempotente) → se outro estado, `ErrNaoCancelavel` → ingresso `usado` → `ErrNaoCancelavel` → fora da janela → `ErrForaDaJanela` → CAS `pago → estorno_pendente` (motivo `cancelamento`) + `pedido_eventos` + ingressos `ativo → cancelado`. Métrica `cancelamentos_total{origem="cliente"}` depois do commit (só quando transicionou).
- RF05 — `POST /pedidos/:id/cancelar` (Bearer obrigatório — mesmo `exigirConta` de "Meus pedidos"; anti-CSRF): só pedido da conta (`usuario_id` do JWT); alheio/inexistente → 404 idêntico. 200 → visão do pedido.
- RF06 — `POST /pedidos/consulta/cancelar {email, codigo}` (convidado; anti-CSRF, corpo ≤ 4 KB, campos desconhecidos recusados, `no-store`): mesmo rate limit e mesmo caminho em tempo constante da consulta (PRD 0034); todo "não encontrado" idêntico ao da consulta. 200 → mesmo formato da consulta (`pedido` + `ingressos` — agora vazio).
- RF07 — Erros: `ErrNaoCancelavel` → `409 {"erro":"nao_cancelavel"}`; `ErrForaDaJanela` → `409 {"erro":"fora_da_janela"}`.
- RF08 — `cancelavel` (bool) na visão do pedido (`GET /pedidos/:id`, "Meus pedidos", consulta): `status = pago` e `agora ≤ inicio − 2h`, calculado no servidor (o SPA não usa o relógio do navegador).
- RF09 — Porta `FonteSessoes.InicioDaSessao(ctx, id) (inicio, cancelada, ok, err)` implementada pelo `sessao` (qualquer status).
- RF10 — Cancelar sessão (`POST /backoffice/sessoes/:id/cancelar`), numa TX do `sessao`: trava a sessão → inexistente → 404; agendada e já iniciada → `409 {"erro":"sessao_iniciada"}`; marca `cancelada` → chama a porta `PedidosDaSessao.CancelarPedidosDaSessao(ctx, tx, id)` → commit → invalida o cache. Resposta `200 {"pedidos_estornados": n}`; repetir → 200 com 0 (idempotente).
- RF11 — A porta (implementada no `pedido`) trava **todas** as linhas de pedido da sessão (`FOR UPDATE`, ordem por id) e move os `pago` para `estorno_pendente` (`sessao_cancelada`) com os ingressos cancelados e a trilha; métrica `origem="sessao"` por pedido, depois do commit (callback do `sessao`).
- RF12 — Pivô: com o pedido travado e ainda `aguardando_pagamento`, sessão cancelada → `estorno_pendente` (`sessao_cancelada`) em vez de emitir. Fecha a corrida: a TX da sessão espera a trava do pivô (RF11) e o pivô lê a sessão depois de travar o pedido.

## Requisitos não funcionais

- RNF01 — Nenhum e-mail, código ou token em log; logs com `pedido_id`/`sessao_id`.
- RNF02 — Depguard: `sessao` passa a poder importar `internal/outbox` (só `Pool`/`Tx`/`WithTx`, como a `reserva`); `pedido` continua sem importar outros módulos.

## Regras de negócio

- RN01 — Só pedido `pago` é cancelável; o pendente expira sozinho.
- RN02 — Assentos voltam à venda só no `estornado` (ADR 0010/0011); o `/i/*` responde 410 logo após o cancelamento.
- RN03 — O operador cancela a sessão sem janela, mas não depois de ela começar.

## Critérios de aceite

- [x] CA01 — Tabela de transições: `pago + CancelamentoSolicitado → estorno_pendente`; o evento é ilegal em todos os outros estados.
- [x] CA02 — Fronteira: início − 2h01 e − 2h00 cancelável; − 1h59 e sessão iniciada não (unit com relógio fixo).
- [x] CA03 — Logado cancela o próprio pedido → 200 `estorno_pendente`, `cancelavel=false`, ingressos cancelados, `/i/*` → 410, métrica `cliente`; repetir → 200 sem segundo evento na trilha; pedido de outra conta → 404; sem token → 401; sem anti-CSRF → 403.
- [x] CA04 — Convidado cancela por e-mail + código → 200 com `ingressos: []`; e-mail errado/código inexistente → idênticos ao 404 da consulta; teto de requisições compartilhado com a consulta.
- [x] CA05 — Fora da janela → 409 `fora_da_janela`; pedido `aguardando_pagamento` → 409 `nao_cancelavel`; ingresso `usado` → 409.
- [x] CA06 — Ciclo completo: cancelado → job de estorno → `estornado`, assentos liberados (novo hold no mesmo assento aceito), evento `pedido.estornado` na outbox.
- [x] CA07 — Corrida: N cancelamentos concorrentes do mesmo pedido → exatamente 1 transição e 1 estorno no gateway.
- [x] CA08 — Sessão com pedidos em estados mistos (pago ×2, aguardando, expirado, estornado) → só os pagos vão a `estorno_pendente` (`sessao_cancelada`), ingressos cancelados; resposta `pedidos_estornados: 2`; repetir → 0; sessão iniciada → 409; inexistente → 404.
- [x] CA09 — Pagamento confirmado depois do cancelamento da sessão → `estorno_pendente` (`sessao_cancelada`), nenhum ingresso emitido.
- [x] CA10 — Lint, suíte `-race` e CI verdes; M3/M4 intactos.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Transições e regra da janela | unit (`pedido`) | CA01, CA02 |
| Rotas do cliente/convidado, ciclo, corrida, pivô | integração (`pedido`, PG + Redis reais, sessao/reserva reais) | CA03–CA07, CA09 |
| Cancelar sessão com pedidos (porta real ligada como no main) | integração (`pedido`) | CA08 |
| Cancelar sessão sem pedidos, 409/404, cache | integração (`sessao`) | CA08 |

## Plano de implementação

1. Migration 016 + estados + regra de domínio + testes unitários.
2. Queries + sqlc; `cancelamento.go` (serviço + porta da sessão), `cancelavel` nas visões.
3. Handler (rotas logado/convidado) e pivô.
4. `sessao`: TX no cancelamento, porta, 409; main (porta, métrica, depguard).
5. Testes de integração; docs (`doc.md` estados).

**Skills de apoio (§4.4):** `golang-database`, `golang-testing`.

## Arquivos que serão criados

- `migrations/016_motivo_cancelamento.up.sql`, `migrations/016_motivo_cancelamento.down.sql`
- `internal/pedido/cancelamento.go`, `internal/pedido/cancelamento_test.go`
- `docs/tasks/0036-cancelamento.md`, `docs/prd/0036-cancelamento.md`

## Arquivos que serão modificados

- `internal/pedido/{estados.go, errors.go, queries.sql, service.go, repositorio.go, pivo.go, handler.go, consulta.go, dominio_test.go, handler_test.go}`, `internal/pedido/db/queries.sql.go` (gerado)
- `internal/sessao/{service.go, handler.go, errors.go, queries.sql, handler_test.go}`, `internal/sessao/db/queries.sql.go` (gerado)
- `internal/reserva/{pedido.go, queries.sql, pedido_test.go}`, `internal/reserva/db/queries.sql.go` (gerado) — desvio achado pelos testes: `LiberarDoPedido` devolve também os holds vendidos no estorno
- `cmd/morfeu/main.go`, `.golangci.yml`
- `doc.md`, `docs/tasks/README.md`, `plan.md`, `state.md`

Total previsto ~28; real 32 (29 autorais + 3 gerados — ver plan.md).

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- `POST /backoffice/sessoes/:id/cancelar` muda de 204 para 200 com corpo (só a API usa hoje; telas na 0038).
- O pivô faz uma leitura a mais (sessão) por webhook — índice pela PK.
- Nova série `cancelamentos_total{origem}` (iniciada em 0).

## Riscos

- Corrida pivô × cancelamento da sessão — fechada pela ordem de travas (RF11/RF12) e coberta por teste.
- Caminho `pago → estornado` nunca exercitado antes — CA06.

## Estratégia de rollback

Reverter o merge e aplicar o down da 016 (só se nenhum pedido tiver os motivos novos — senão o down falha de propósito).
