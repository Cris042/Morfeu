# Task 0010 — Refresh rotativo com detecção de reuso + deleção por pseudonimização (E1, T2)

- **Data:** 2026-09-29
- **Status:** concluída
- **Branch:** `feature/0010-refresh-pseudonimizacao` (da main `3ef3188`, pós-0009)
- **PRD:** docs/prd/0010-refresh-pseudonimizacao.md
- **Item do roadmap:** E1 — Identidade (T2 — fecha o épico). Refinamento: `docs/refinamentos/E1-identidade.md` §T2 + respostas do usuário (7 dias, SameSite Strict, sem janela de graça).

## Objetivo

Sessão longa segura: refresh token opaco rotativo em cookie, detecção de reuso que revoga a família, logout server-side, deleção de conta por pseudonimização (LGPD, doc.md §7) e limpeza periódica no worker.

## Escopo

Migration 006 `refresh_token`; login passa a emitir o cookie; `POST /auth/refresh`, `POST /auth/refresh/logout`, `DELETE /auth/conta`; métrica + alerta de reuso; limpeza no worker; testes (corrida 20× sob `-race`).

## Fora de escopo

Single-flight no SPA (E8); rotação de chave JWT (Fase 2); "sair de todos os dispositivos".

## Arquivos esperados

~20: migration (2) · identidade (queries, db ×2, sessao.go, handler, service, testes) · main.go · `.golangci.yml` · alertas + teste da stack + runbook · controle (5).

## Critérios de aceite

- [ ] Login seta cookie `httpOnly; Secure; SameSite=Strict; Path=/auth/refresh`, 7 dias; só o SHA-256 vai ao banco.
- [ ] Refresh rotaciona (antigo usado, sucessor na mesma família); sem header anti-CSRF → 403.
- [ ] Reuso → família revogada, 401, métrica `auth_refresh_reuso_total`, alerta provisionado.
- [ ] Corrida de 2 refresh com o mesmo token → exatamente 1 sucesso e família revogada — 20/20 sob `-race`.
- [ ] Logout revoga a família e limpa o cookie.
- [ ] Deleção: `nome`/`email` pseudonimizados com valores exatos, senha inutilizada, famílias revogadas, e-mail liberado para novo cadastro.
- [ ] Limpeza remove expirados; CI verde.

## Riscos

- Corrida mal serializada → `SELECT ... FOR UPDATE` na mesma TX + teste 20×.

## Estimativa de impacto

Médio em código, baixo em banco (tabela nova), nenhum em infra.
