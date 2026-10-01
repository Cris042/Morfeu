# PRD 0038 — SPA: cancelar pedido (cliente) + backoffice de filmes e salas (E9, T3a)

- **Task:** docs/tasks/0038-spa-cancelar-backoffice.md
- **Branch:** feature/0038-spa-cancelar-backoffice
- **Data:** 2026-10-01
- **Status:** em andamento

## Objetivo

Fontes: `docs/refinamentos/E9-backoffice.md` §T1 (botão do cliente, E2E do cancelamento — movidos da 0036) e §T3 (backoffice mínimo); ADR 0009 (SPA), ADR 0011; APIs das tasks 0011–0014 e 0036–0037.

## Escopo

Cancelamento pelo cliente na SPA; área do operador com filmes e salas.

## Fora de escopo

- Sessões e pedidos do operador; E2E do operador (0039).
- Edição manual de filme (o refinamento E9 fixou "sem formulário manual": importar do TMDB + arquivar).

## Requisitos funcionais

- RF01 — `Pedido.cancelavel` (do servidor) controla o botão "Cancelar pedido" no detalhe de "Meus pedidos" e no resultado da consulta de convidado; o SPA nunca calcula a janela pelo relógio do navegador.
- RF02 — Confirmação explícita ("Sim, cancelar" / "Manter pedido"); mensagens: `fora_da_janela`, `nao_cancelavel`, 429 e genérica, em `role="alert"`; após cancelar, o pedido mostra "Estorno em andamento" e a nota de devolução.
- RF03 — Convidado: e-mail e código só em memória (estado do componente), reaproveitados no `POST /pedidos/consulta/cancelar`; nunca em storage.
- RF04 — `/backoffice/*` em chunk lazy; visitante → login com volta; cliente → "Área restrita" sem nenhuma chamada ao backoffice; operador → abas Filmes e Salas; link "Backoffice" no menu só para operador. O guarda é só experiência (a API decide).
- RF05 — Filmes: lista (inclui arquivados), busca no TMDB e importação (dados externos só como texto), arquivar com confirmação; mensagens dos erros do TMDB.
- RF06 — Salas: lista; criar/editar com nome + layout em JSON num textarea (modelo pré-preenchido); JSON inválido barrado antes da API; `nome_em_uso`, `layout_em_uso`, `dados_invalidos` explicados.
- RF07 — E2E (Playwright, `m4.spec.ts`): convidado compra D5, consulta, cancela; o link do ingresso responde 410; o assento volta a "livre" depois do job de estorno; axe e CSP limpos.

## Requisitos não funcionais

- RNF01 — Sem dependência nova; CSS Modules com os tokens; CSP de produção intacta (sem `style={{}}`).
- RNF02 — O chunk do backoffice não entra no bundle inicial.

## Critérios de aceite

- [ ] CA01 — Conta: cancelável mostra o botão; confirmar chama `POST /pedidos/{id}/cancelar` com Bearer e o detalhe recarrega sem o botão; não cancelável não mostra; 409 `fora_da_janela` explicado.
- [ ] CA02 — Convidado: cancela com as mesmas credenciais; storage vazio; ingressos somem.
- [ ] CA03 — Guarda: visitante → login; cliente → área restrita sem chamadas; operador entra pelo menu.
- [ ] CA04 — Filmes: lista/arquivado; busca e importação (corpo `{"tmdb_id":…}`); tag do TMDB exibida como texto; arquivar só após confirmar; TMDB fora do ar explicado.
- [ ] CA05 — Salas: JSON inválido não chega à API; criação envia nome + layout; `layout_em_uso` explicado na edição.
- [ ] CA06 — Lint, typecheck, testes e build verdes; chunk `Backoffice-*.js` separado.
- [ ] CA07 — E2E do cancelamento verde no CI (M3/M4 intactos).

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Cancelar na conta e na consulta | componente (Vitest) | CA01, CA02 |
| Guarda, filmes, salas | componente (Vitest, App inteiro) | CA03–CA05 |
| Cancelamento do convidado ponta a ponta | E2E (Playwright, CI) | CA07 |

## Plano de implementação

1. Tipos/API (`cancelavel`, cancelar, `api.put`, `backoffice.ts`).
2. `CancelarPedido` + conta + consulta.
3. Backoffice (guarda, filmes, salas) + rota lazy + menu.
4. Testes, E2E.

**Skills de apoio (§4.4):** `frontend-patterns`, `e2e-testing`.

## Arquivos que serão criados

- `web/src/api/backoffice.ts`, `web/src/ui/CancelarPedido.{tsx,module.css}`
- `web/src/features/backoffice/{Backoffice.tsx, Backoffice.module.css, Filmes.tsx, Salas.tsx, Backoffice.test.tsx}`
- `docs/tasks/0038-spa-cancelar-backoffice.md`, `docs/prd/0038-spa-cancelar-backoffice.md`

## Arquivos que serão modificados

- `web/src/api/{client.ts, conta.ts, consulta.ts, tipos.ts}`, `web/src/app/{App.tsx, MenuConta.tsx}`
- `web/src/features/conta/{MeusPedidos.tsx, Conta.test.tsx}`, `web/src/features/consulta/{Consulta.tsx, Consulta.test.tsx}`, `web/e2e/m4.spec.ts`
- `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 24.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- O E2E do M4 ganha um cenário que espera o job de estorno (até ~2 min).

## Riscos

- E2E dependente do relógio do worker → espera com teto de 150 s.

## Estratégia de rollback

Reverter o merge (só SPA).
