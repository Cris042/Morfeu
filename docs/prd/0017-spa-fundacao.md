# PRD 0017 — SPA: fundação (shell, tokens, cliente `/api`, CI do front) (E5, T1)

- **Task:** docs/tasks/0017-spa-fundacao.md
- **Branch:** feature/0017-spa-fundacao
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Criar em `web/` a base da SPA definida no ADR 0009, sobre a qual as tasks 0018–0020 constroem cartaz, mapa e M3:
- React + TypeScript estrito + Vite;
- navegador chamando `/api/*`, com o proxy removendo o prefixo;
- identidade visual "A Sala Escura" como tokens CSS e fontes self-hosted;
- um único cliente HTTP com as regras anti-CSRF do E4;
- CI do front com auditoria de dependências como gate.

Fontes: `docs/refinamentos/E5-spa-cliente.md` §T1 e as decisões do usuário (ADR 0009; headers definidos aqui e aplicados no E0c-CD).

## Escopo

- scaffold `web/`;
- `tokens.css` e `@font-face`;
- `api/client.ts` com testes;
- shell (`BrowserRouter` + `QueryClientProvider` + layout);
- ESLint e Vitest;
- workflow `web-ci`;
- snippet de headers de segurança do Caddy;
- documentação de ambiente;
- `lib.md`.

## Fora de escopo

- telas de cartaz e sessões (0018);
- mapa de assentos (0019);
- holds, polling e Playwright (0020);
- Caddyfile completo e aplicação real dos headers (E0c-CD);
- login no SPA (E8).

## Requisitos funcionais

- RF01 — **Scaffold** `web/`:
  - Node 24 LTS em `.nvmrc` + `engines` (`>=24 <25`);
  - npm com `package-lock.json` versionado;
  - scripts `dev`, `build` (`tsc --noEmit && vite build`), `preview`, `lint`, `typecheck`, `test` (`vitest run`);
  - dependências com versão exata (sem `^`), para build reprodutível.
- RF02 — **TypeScript estrito**: `strict`, `noUncheckedIndexedAccess`, `noUnusedLocals`, `noUnusedParameters`, `verbatimModuleSyntax`; `types: ["vite/client"]` (tipos de CSS Modules e assets sem `.d.ts` próprio).
- RF03 — **Proxy**: `server.proxy['/api']` → `http://localhost:8080` (porta padrão da API em dev e no compose; sem variável de ambiente — evitaria `@types/node` só para isso), `changeOrigin: true`, `rewrite` removendo `^/api`. O Vite repete o mesmo proxy no `vite preview` (usado pelo E2E da 0020).
  - O cookie `morfeu_carrinho` usa `Path=/` (PRD 0015). Como o navegador só vê a origem do Vite/Caddy, o cookie vale para `/api/*` sem ajuste — verificação manual registrada no plano.
  - Nenhuma resposta da API usa `Location` absoluto.
- RF04 — **`src/api/client.ts`**: único ponto de rede.
  - `api.get<T>(caminho)`, `api.post<T>(caminho, corpo?)`, `api.delete(caminho)`.
  - Base `/api`; `credentials: 'include'` em toda chamada.
  - `X-Requested-With: morfeu` em todo método que muda estado (POST/PUT/PATCH/DELETE), nunca em GET.
  - `Content-Type: application/json` quando há corpo.
  - Resposta não-2xx → `ErroApi { status, codigo (campo "erro" do corpo, se houver), corpo }`; falha de rede → `ErroApi` com `status: 0`.
  - 204 → `undefined`.
  - Nenhum componente chama `fetch` direto (regra de ESLint `no-restricted-globals` para `fetch` fora de `src/api/`).
- RF05 — **Shell**:
  - `main.tsx` monta `BrowserRouter` + `QueryClientProvider` (`retry: 1`, `refetchOnWindowFocus: false`);
  - `App.tsx` traz o cabeçalho com a marca, o `<main>` com as rotas (por ora `/` com um placeholder "Em cartaz" e `*` com 404 amigável) e o rodapé com a atribuição do TMDB (texto fixo exigido desde o E2);
  - idioma `pt-BR` no `index.html`.
- RF06 — **Identidade visual** (`src/styles/tokens.css`, global):
  - os 7 tokens de cor, tipografia (famílias + escala), `color-scheme: dark`, fundo `--noite`, foco visível em `--tungstenio`;
  - `@media (prefers-reduced-motion: reduce)` zerando animações e transições;
  - 4 woff2 em `web/public/fonts/` (Fraunces normal e itálico, Schibsted Grotesk, Spline Sans Mono — fontes variáveis, subset latin) com `font-display: swap`. **Origem verificada**: os SHA-256 batem com os arquivos oficiais de `fonts.gstatic.com` e com os blobs do protótipo aprovado;
  - licença SIL OFL 1.1 das 3 famílias em `web/public/fonts/OFL.txt`.
- RF07 — **ESLint** (flat config):
  - base: `@eslint/js` recommended + `typescript-eslint` (type-checked) + `eslint-plugin-react-hooks`;
  - **`no-restricted-syntax` barra o atributo JSX `dangerouslySetInnerHTML`** (equivale a `react/no-danger`; o `eslint-plugin-react` não suporta ESLint 10 — uma dependência a menos);
  - barra `localStorage`/`sessionStorage` (`no-restricted-globals`).
- RF08 — **Workflow `.github/workflows/web-ci.yml`**:
  - dispara em PR/push quando `web/**` ou o próprio workflow mudam;
  - `ubuntu-24.04-arm`, actions pinadas por SHA (padrão do `ci.yml`);
  - `actions/setup-node` com `node-version-file: web/.nvmrc` e `cache: npm`;
  - passos: `npm ci` → `lint` → `typecheck` → `test` → `build` → **`npm audit --audit-level=high`** (gate);
  - `permissions: contents: read`; meta < 2 min.
- RF09 — **Headers de segurança de produção** em `configs/caddy/seguranca.caddy`, um snippet importável pelo Caddyfile do E0c-CD:
  - `Content-Security-Policy: default-src 'self'; img-src 'self' https://image.tmdb.org; connect-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`;
  - `X-Content-Type-Options: nosniff`;
  - `Referrer-Policy: strict-origin-when-cross-origin`;
  - `Strict-Transport-Security` comentado até o TLS existir.
- RF10 — **Documentação**:
  - `docs/ambiente-dev.md` ganha a seção do front (nvm, `npm ci`, `npm run dev` com a API em `:8080`, testes e lint);
  - `lib.md` registra cada dependência (versão, papel, alternativa, riscos, fonte consultada) **antes** do `npm install`, com as fontes como ativo estático.
- RF11 — `.gitignore` ignora `web/node_modules/` e `web/dist/`.

## Requisitos não funcionais

- RNF01 — Nenhum dado de sessão/carrinho no `localStorage`/`sessionStorage` (lint).
- RNF02 — Testes sem rede (fetch substituído por dublê em `vi.stubGlobal`).
- RNF03 — O CI Go (`ci.yml`) não muda. O backend não muda.
- RNF04 — Sem CDN externa: fontes, JS e CSS saem da mesma origem, compatível com a CSP do RF09.

## Regras de negócio

- RN01 — Escrita sem `X-Requested-With` é recusada pela API (E4). O cliente garante o header em todo método que muda estado.

## Critérios de aceite

- [ ] CA01 — `npm ci && npm run lint && npm run typecheck && npm test && npm run build && npm audit --audit-level=high` verdes localmente.
- [ ] CA02 — `client.test.ts`:
  - GET sem `X-Requested-With` e com `credentials: 'include'`;
  - POST/DELETE com o header e JSON;
  - 409 → `ErroApi` com `codigo` e `corpo`;
  - 204 → `undefined`;
  - falha de rede → `status 0`.
- [ ] CA03 — Teste do shell: renderiza a marca, o placeholder do cartaz e a atribuição TMDB; rota desconhecida → 404 amigável.
- [ ] CA04 — Lint falha com `dangerouslySetInnerHTML`, com `fetch` fora de `src/api/` e com `localStorage` (verificado no desenvolvimento; regras no `eslint.config.js`).
- [ ] CA05 — Com a API rodando, `npm run dev` + `curl localhost:5173/api/filmes` retorna o cartaz (proxy com strip). Verificado à mão e registrado no plano.
- [ ] CA06 — `web-ci` verde no PR (ARM64).
- [ ] CA07 — CI Go verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Cliente HTTP (headers, credentials, erros) | unit (Vitest, fetch dublê) | CA02 |
| Shell e rotas | componente (Testing Library) | CA03 |
| Proxy `/api` com a API real | manual registrado | CA05 |

## Plano de implementação

1. `lib.md` com as dependências + Context7/registro/OSV.
2. Scaffold (`package.json` exato, `tsconfig`, `vite.config.ts` com Vitest, `eslint.config.js`, `index.html`, `.nvmrc`); `npm install` gera o lockfile.
3. `tokens.css` + fontes + OFL; `client.ts` + testes; shell + teste.
4. `web-ci.yml`; `seguranca.caddy`; `ambiente-dev.md`; `.gitignore`.
5. Gate local; proxy verificado com a API.

**Skills de apoio (§4.4):** `frontend-patterns`, `design-system`.

## Arquivos que serão criados

- `web/package.json`, `web/package-lock.json`, `web/.nvmrc`, `web/tsconfig.json`, `web/vite.config.ts`, `web/eslint.config.js`, `web/index.html`
- `web/src/main.tsx`, `web/src/app/App.tsx`, `web/src/app/App.test.tsx`, `web/src/styles/tokens.css`, `web/src/api/client.ts`, `web/src/api/client.test.ts`, `web/src/test/setup.ts`
- `web/public/fonts/{fraunces.woff2, fraunces-italico.woff2, schibsted-grotesk.woff2, spline-sans-mono.woff2, OFL.txt}`
- `.github/workflows/web-ci.yml`, `configs/caddy/seguranca.caddy`
- `docs/tasks/0017-spa-fundacao.md`, `docs/prd/0017-spa-fundacao.md`

## Arquivos que serão modificados

- `.gitignore`, `docs/ambiente-dev.md`, `lib.md`, `docs/design/identidade-visual.md` (pendência das fontes resolvida), `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 30.

## Dependências utilizadas

Todas novas, com versão exata e registradas no `lib.md`:

- **Runtime:**
  - react/react-dom 19.3.0
  - react-router 8.4.0
  - @tanstack/react-query 5.104.0
- **Dev:**
  - vite 8.3.1
  - @vitejs/plugin-react 6.1.1
  - typescript 6.0.3 — a 7.x é incompatível com o typescript-eslint, que exige `<6.1`
  - vitest 5.0.2
  - jsdom 30.1.1
  - @testing-library/react 16.3.3
  - @testing-library/dom 10.4.2
  - @testing-library/user-event 14.6.7
  - @testing-library/jest-dom 7.0.1
  - eslint 10.11.0
  - @eslint/js 10.0.1
  - typescript-eslint 8.71.0
  - eslint-plugin-react-hooks 7.1.1
  - globals 17.12.0
  - @types/react 19.3.0
  - @types/react-dom 19.3.0

**Descartada:** `eslint-plugin-react`, cujo peer é `eslint ^9.7`; a regra dele é substituída por `no-restricted-syntax`. O Playwright entra na 0020.

## Impactos técnicos

- Surge uma segunda árvore de dependências (npm), isolada em `web/`.
- O CI ganha um workflow independente, com filtro de caminho.
- Nenhum impacto no backend.

## Riscos

- **Versões muito novas:** incompatibilidades de peer. Mitigação: peers conferidos no registro e `npm ci` no CI.
- **Proxy de dev diferente do Caddy:** mitigação com o E2E da 0020 pelo proxy e o smoke do E0c-CD.
- **`npm audit` falhar por advisory transitivo sem correção:** tratar caso a caso no PR (override documentado), nunca desligar o gate.

## Estratégia de rollback

Reverter o merge. `web/` é isolado, e o CI Go e o backend não mudam.
