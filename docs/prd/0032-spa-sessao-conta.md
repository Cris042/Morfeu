# PRD 0032 — SPA: sessão, login/cadastro e "Meus pedidos" (E8, T2)

- **Task:** docs/tasks/0032-spa-sessao-conta.md
- **Branch:** feature/0032-spa-sessao-conta
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Conta opcional no SPA sobre a API do E1 e da 0031. O access token (JWT de 10 min) fica só na memória da aba e a sessão sobrevive ao recarregar pelo cookie de refresh (HttpOnly, rotativo, com detecção de reuso). Fontes: `docs/refinamentos/E8-spa-checkout.md` §T2, refinamento E1 (single-flight entre abas), ADR 0009 (fronteira `/api`).

## Escopo

`web/src/api/sessao.ts`, cliente HTTP com Bearer, telas de conta, menu, proxy e E2E.

## Fora de escopo

- Tela de checkout e o e-mail pré-preenchido nela (0033 lê `useSessao()`).
- Consulta de convidado e página do ingresso (0034/0035).
- Remover conta pela UI.

## Requisitos funcionais

- RF01 — **Token só em memória**: variável do módulo `sessao.ts`; nunca `localStorage`/`sessionStorage`/cookie legível (lint já proíbe storage; teste confere).
- RF02 — **Refresh único** (`criarRefreshUnico`, puro, dependências injetadas):
  - uma promessa por aba — N chamadas simultâneas → 1 `POST /auth/refresh`;
  - entre abas, Web Lock `morfeu-refresh`; dentro do lock o estado é relido e, se outra aba publicou resultado enquanto esperávamos, ele é reaproveitado sem chamar o servidor;
  - falha transitória rejeita todos os que esperavam e a próxima chamada tenta de novo;
  - 401 do refresh → deslogado (sem laço);
  - sem Web Locks: só o single-flight da aba (colisão entre abas derruba a família — falha segura).
- RF03 — **BroadcastChannel** `morfeu-sessao` só propaga resultado (`sessao` com a credencial) e `saiu`.
- RF04 — **Cliente HTTP**: Bearer automático quando logado (renovado antes se faltarem < 30 s); 401 de requisição que levou o token automático → 1 refresh + 1 nova tentativa; rotas `/auth/*` nunca levam o Bearer automático; `X-Requested-With` em toda escrita (inclusive refresh/logout).
- RF05 — **Boot**: `iniciarSessao()` no `main.tsx`, sem bloquear o cartaz; rede/5xx no boot → visitante.
- RF06 — **Telas**:
  - `/entrar` e `/cadastro` (cadastro entra em seguida); erros: credenciais inválidas (mensagem única), e-mail em uso, campos inválidos, 429, rede.
  - `?volta=` só aceita caminho local (`/…`, nunca `//` ou `/\`) — sem redirecionamento aberto.
  - `/conta/pedidos` (paginado de 20, "Mais antigos"/"Mais recentes") e `/conta/pedidos/:id` (situação, assentos, total, prazo se aguardando; 404 → "não encontrado nesta conta").
  - Visitante em rota da conta → `/entrar?volta=…`.
  - Menu no topo: "Entrar" ou nome + "Meus pedidos" + "Sair"; ao sair (aqui ou em outra aba) o cache `['conta', …]` é removido; a chave inclui o id da conta.
- RF07 — **Proxy**: o Echo emite o cookie com `Path=/auth/refresh`, mas o navegador o vê em `/api/auth/refresh` — sem reescrita ele nunca volta. Vite (`server` e `preview`): `cookiePathRewrite: { '/auth/refresh': '/api/auth/refresh' }`. Caddy (E0c-CD): mesma reescrita no `Set-Cookie` (registrado no `ambiente-dev.md` e no `state.md`). O backend continua sem conhecer `/api` (ADR 0009).

## Requisitos não funcionais

- RNF01 — Sem dependência nova.
- RNF02 — Acessibilidade: rótulos nos campos, erro em `role="alert"`, navegação "Conta" nomeada, carregamento em `role="status"`.
- RNF03 — Sem `style={{}}` inline nem `dangerouslySetInnerHTML`.

## Regras de negócio

- RN01 — A conta é opcional: nada da compra exige login.
- RN02 — "Meus pedidos" mostra só os pedidos da conta (a API garante; o SPA não guarda pedidos de outra conta no cache).

## Critérios de aceite

- [ ] CA01 — Unit (puro): 5 chamadas → 1 refresh; falha propagada + retry; duas "abas" com lock e servidor que detecta reuso → 1 chamada, nenhuma revogação; sem lock → família derrubada (documenta a falha segura).
- [ ] CA02 — Unit (módulo): login → Bearer nas chamadas, storage vazio, `/auth/*` sem Bearer; 401 → 1 refresh para 3 chamadas + retry com o token novo; refresh 401 → anônimo e nenhuma nova tentativa depois; boot restaura / vira visitante; token vencido renovado antes; logout propagado a outra instância via BroadcastChannel.
- [ ] CA03 — Telas: login com volta; mensagem única de credenciais; campos inválidos e 429 no cadastro; cadastro entra; visitante redirecionado com volta; lista, paginação, detalhe com Bearer, 404; sair limpa e volta ao cartaz; `destinoSeguro` recusa `//`, `/\` e URL absoluta.
- [ ] CA04 — E2E: login numa aba; duas abas recarregam juntas → todas as respostas do refresh 200, ambas logadas, recarga posterior ainda logada; logout em uma desloga a outra e a recarga vira visitante.
- [ ] CA05 — Lint, typecheck, testes, build e CI (web-ci + E2E) verdes.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| `criarRefreshUnico` com dublês de lock/servidor | unit | CA01 |
| Módulo de sessão + cliente contra fetch falso | unit | CA02 |
| Telas pelo `App` com `apiFalsa` | componente | CA03 |
| Duas páginas no mesmo contexto contando `POST /api/auth/refresh` | E2E (Playwright, stack real) | CA04 |

## Plano de implementação

1. Proxy (`cookiePathRewrite`).
2. `client.ts` (Autenticador injetado, Bearer, retry) + `sessao.ts`.
3. Hooks da conta, telas, menu, rotas, boot.
4. Testes unitários/componente, E2E, docs.

**Skills de apoio (§4.4):** `frontend-patterns`, `e2e-testing`.

## Arquivos que serão criados

- `web/src/api/sessao.ts`, `web/src/api/sessao.test.ts`, `web/src/api/conta.ts`
- `web/src/features/conta/{Entrar.tsx, MeusPedidos.tsx, Conta.module.css, Conta.test.tsx}`
- `web/src/app/MenuConta.tsx`, `web/e2e/sessao.spec.ts`
- `docs/tasks/0032-spa-sessao-conta.md`, `docs/prd/0032-spa-sessao-conta.md`

## Arquivos que serão modificados

- `web/vite.config.ts`, `web/src/api/{client.ts, tipos.ts}`, `web/src/app/{App.tsx, App.module.css}`, `web/src/main.tsx`
- `.github/workflows/e2e.yml` (paths de identidade/pedido)
- `docs/tasks/README.md`, `docs/ambiente-dev.md`, `plan.md`, `state.md`

Total: 24.

## Dependências utilizadas

Nenhuma nova. Web Locks e BroadcastChannel são APIs nativas (Chromium/Firefox/Safari atuais); `cookiePathRewrite` é opção do proxy do Vite 8 (conferida no `index.d.ts`).

## Impactos técnicos

- Toda requisição de um usuário logado leva o Bearer (a API ignora nas rotas públicas; `POST /pedidos` passa a vincular a conta — 0031).
- Visitante faz 1 `POST /auth/refresh` (401) por carregamento de página.

## Riscos

- Reuso do cookie entre abas sem Web Locks → família revogada → o usuário entra de novo (falha segura).
- Caddy sem a reescrita do `Path` → re-auth silenciosa quebra em produção (registrado para o E0c-CD).

## Estratégia de rollback

Reverter o merge: o SPA volta a não ter conta; nenhuma migration nem contrato da API muda.
