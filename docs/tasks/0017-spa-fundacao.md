# Task 0017 — SPA: fundação (shell, tokens, cliente `/api`, CI do front) (E5, T1)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `feature/0017-spa-fundacao` (da main `67d1d0b`)
- **PRD:** docs/prd/0017-spa-fundacao.md
- **Item do roadmap:** E5 — SPA cliente, parte 1 (task 1/4). Refinamento: `docs/refinamentos/E5-spa-cliente.md` §T1; ADR 0009.

## Objetivo

Criar a base da SPA em `web/` — o shell que o E0c nunca entregou. Inclui React + TypeScript estrito + Vite, o proxy `/api` para a API Go, os tokens e as fontes da identidade visual, o cliente HTTP único e o workflow de CI do front com auditoria de dependências. Também define a política de headers de segurança para o deploy.

## Escopo

Scaffold `web/`, `tokens.css` com 4 fontes self-hosted (origem verificada), `api/client.ts` + testes, shell com roteador e QueryClient, ESLint (bloqueio de `dangerouslySetInnerHTML`), Vitest, workflow `web-ci`, snippet de headers do Caddy, seção do front no `docs/ambiente-dev.md`, `lib.md`.

## Fora de escopo

Telas de cartaz e sessões (0018); mapa (0019); holds e E2E (0020); deploy/Caddyfile completo (E0c-CD).

## Arquivos esperados

~29 (lista no PRD).

## Dependências esperadas

Novas (npm): react, react-dom, react-router, @tanstack/react-query; dev: vite, @vitejs/plugin-react, typescript, vitest, jsdom, @testing-library/{react,dom,user-event,jest-dom}, eslint, @eslint/js, typescript-eslint, eslint-plugin-react-hooks, globals, @types/react, @types/react-dom — todas no `lib.md` antes do `npm install`.

## Critérios de aceite

- [ ] `npm ci`, lint, typecheck, test e build verdes localmente e no `web-ci` (ARM64), com `npm audit --audit-level=high` limpo.
- [ ] `npm run dev` + API local: `/api/filmes` responde via proxy (strip do prefixo).
- [ ] `client.ts`: `credentials: 'include'` sempre, `X-Requested-With` só nas escritas, erro tipado com corpo — testado.
- [ ] ESLint barra `dangerouslySetInnerHTML`.
- [ ] Política de headers de segurança versionada.
- [ ] CI Go intocado e verde.

## Riscos

- Árvore npm grande → lockfile + audit gate + só dependências registradas.

## Estimativa de impacto

Médio em código (árvore nova, isolada), nenhum em backend/banco, baixo em CI (workflow novo com filtro).
