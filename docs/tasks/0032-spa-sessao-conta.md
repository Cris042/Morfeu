# Task 0032 — SPA: sessão, login/cadastro e "Meus pedidos" (E8, T2)

- **Data:** 2026-09-30
- **Status:** concluída (auditoria APROVADA em 2026-09-30)
- **Branch:** `feature/0032-spa-sessao-conta` (da main `802e374`)
- **PRD:** docs/prd/0032-spa-sessao-conta.md
- **Item do roadmap:** E8 — SPA checkout + convidado/conta (2ª de 5). Refinamento: `docs/refinamentos/E8-spa-checkout.md` §T2.

## Objetivo

Dar ao SPA a conta opcional: login, cadastro, logout, sessão restaurada pelo cookie de refresh (re-auth silenciosa) com refresh único entre abas, e "Meus pedidos" (lista + detalhe).

## Escopo

Módulo de sessão (token só em memória, refresh único com Web Locks + BroadcastChannel); cliente HTTP com Bearer e 1 retry após 401; telas de login/cadastro/"Meus pedidos"; menu da conta; reescrita do `Path` do cookie de refresh no proxy `/api`; E2E de duas abas.

## Fora de escopo

Checkout e e-mail pré-preenchido na tela de checkout (0033 — consome `useSessao`); consulta de convidado e ingresso (0034/0035); remoção de conta pela UI.

## Arquivos esperados

19 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [x] Token só em memória (assert de storage vazio no teste unitário e no E2E).
- [x] N chamadas simultâneas → 1 refresh; falha propagada e nova tentativa depois; lock entre abas sem reuso do cookie.
- [x] 401 com token → 1 refresh + 1 retry; refresh 401 → deslogado sem laço.
- [x] Sessão restaurada no boot; logout propagado entre abas.
- [x] Login, cadastro, "Meus pedidos" (lista, paginação, detalhe, 404) e redirecionamento só para caminho local.
- [x] E2E: duas abas restauram juntas sem nenhum refresh recusado.

## Riscos

- Cookie de refresh com `Path=/auth/refresh` nunca volta pelo `/api` → corrigido no proxy (Vite agora; Caddy no E0c-CD).
- Navegador sem Web Locks → só single-flight na aba (colisão derruba a família: falha segura, usuário entra de novo).

## Estimativa de impacto

Médio: só front + proxy de dev; nenhuma mudança de contrato da API.
