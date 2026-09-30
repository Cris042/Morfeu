# Refinamento — Épico E5 (SPA cliente, parte 1) — 2026-09-29

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate → 4 perguntas escaladas e **respondidas pelo usuário no mesmo dia**.

Primeiro épico de frontend: não existe código de SPA no repositório (o "shell da SPA" do E0c foi adiado na task 0004 e nunca nasceu — absorvido pela T1 daqui).

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com 4 tasks | TS estrito; React Router (modo biblioteca); **TanStack Query** (polling com `refetchInterval`, pausa em background, cancelamento, invalidação pós-409); **CSS Modules** + `tokens.css`; Vitest + Testing Library + user-event; npm; Node 24 LTS; fontes woff2 próprias com OFL; `web/` por feature com **um único `api/client.ts`**; **SPA chama `/api/*` e o proxy remove o prefixo** (Echo intocado); mapa = componente puro + container; telas do operador → E9; recomenda ADR |
| security | seguir com ressalvas | cada dependência npm com CVE check + `lib.md` + lockfile; **auditoria npm como gate**; ESLint `react/no-danger`; CSP mínima + nosniff/Referrer-Policy/frame-ancestors **definidas no PRD da T1** (senão "fase 2" nunca chega); dev por proxy, **nunca CORS liberal**; nada do carrinho em `localStorage`; UI não assume sucesso antes da resposta |
| qa | seguir | pirâmide: unit (parser do mapa, estado do assento, contagem regressiva) → componente com fixtures (queries por role/aria) → integração do polling com fetch falso → **E2E só no M3** (2 contextos); **fake timers** sempre; teclado obrigatório; 409 atualiza o mapa fora do ciclo de polling; contagem por `expira_em` com relógio falso |
| sre-devops | seguir | Vite fora do compose com `server.proxy`; Node fixado; job de CI do front paralelo e < 2 min com cache do npm; polling pausa com a aba oculta; E2E do M3 com ambiente efêmero em job próprio; sem ferramenta de erro client-side agora; preferia `/api` no Echo e `embed.FS` |
| backend-dev | seguir | T1 ~20–22 arquivos (**≤ 6 woff2**), T2 ~10–14, T3 ~10–14, T4 ~15–18; strip no proxy (tocar o Echo reescreveria rotas e testes de 4 módulos); nem Caddy nem `embed.FS` agora — **dois processos** (API + Vite) bastam para dev, CI e demo; E2E reusa o profile `app` do compose (migrations já rodam no boot); fontes extraídas do protótipo com verificação contra a origem oficial |

## Debate (divergências e resolução)

1. **Onde fica o prefixo `/api`** — sre: grupo `/api` no Echo × arquiteto/backend-dev: strip no proxy. → **Consenso**: strip no proxy (Vite `rewrite` em dev, `handle_path` no Caddy em prod). A regra é a mesma nos dois ambientes (o argumento do sre), sem reescrever rotas e testes do E1–E4. Guarda: cookie `morfeu_carrinho` com `Path=/` (já é) e nenhuma resposta com `Location` absoluto.
2. **Como a SPA é servida** — sre: `embed.FS` no binário × arquiteto: Caddy. → **Consenso**: em produção, **Caddy serve os estáticos** (`try_files` → `index.html`). O `embed.FS` foi descartado porque acopla o build Go ao Node e muda a imagem estável desde a 0004. Enquanto a VM não existe, dev/CI/demo rodam **dois processos** (API + Vite).
3. **CSP agora × depois** — security: definir já; arquiteto: meta CSP não sustenta política real com HMR. → **Escalado**: usuário escolheu **política definida e versionada no PRD da T1 (alvo do Caddyfile), aplicada no E0c-CD**; `react/no-danger` vale desde a T1.
4. **Quantidade de tasks** — roadmap dizia 2–3; a T1 original estourava 30 arquivos. → **Consenso**: **4 tasks** (fundação; cartaz + sessões; mapa isolado; integração + M3).
5. **Telas do operador** → **Escalado** (muda o roadmap): usuário escolheu **todas no E9**.
6. **E2E do M3 no CI** → **Escalado**: usuário escolheu **CI, job próprio** (dispara quando `web/` ou a reserva mudam).
7. **ADR** → **Escalado**: usuário **autorizou o ADR 0009**.
8. **Single-flight do cartaz** (transferido do E2) — sre: se barato; arquiteto: medir antes. → **Consenso**: segue para o **E12** (k6 mede; épico de front não traz mudança de backend).
9. **Cache de sessões de filme arquivado (≤ 60 s, auditoria 0014)** → **Consenso**: aceito; o front trata 404 ao abrir sessão/mapa sem quebrar.

## Conclusão

4 tasks; **ADR 0009 — Frontend SPA** criado com autorização. Dependências npm novas registradas no `lib.md` na T1 (antes do primeiro `npm install`). Skills `frontend-patterns` e `design-system` reativadas na T1; `e2e-testing` na T4.

### Exigências por task

#### T1 — Fundação da SPA

- `web/` com Vite + React + **TypeScript estrito** (`tsc --noEmit` verde); Node 24 LTS em `.nvmrc` + `engines`; **npm** com `package-lock.json` versionado.
- Estrutura `src/{app,api,features,ui,styles}`; `src/api/client.ts` único: base `/api`, `credentials: 'include'` sempre, `X-Requested-With: morfeu` nas escritas, erros tipados (corpo do 409 exposto); nenhum componente chama `fetch` direto.
- `vite.config.ts`: `server.proxy['/api']` → API local com `rewrite` que remove o prefixo; **sem CORS** no backend.
- Tokens da identidade visual em `styles/tokens.css` (7 cores, tipografia, `prefers-reduced-motion` global); **≤ 6 woff2** self-hosted (Fraunces, Schibsted Grotesk, Spline Sans Mono) + licenças OFL, origem verificada.
- ESLint com `react/no-danger` como erro; Vitest + Testing Library configurados, com teste do `api/client` (headers, credentials, erro tipado).
- **Workflow `web-ci`** (paralelo ao CI Go, filtro `web/**`, runner ARM64, cache do npm): `npm ci` → lint → typecheck → test → build → `npm audit --audit-level=high` (gate). Meta < 2 min.
- **Política de headers de segurança documentada** (alvo do Caddyfile do E0c-CD): `default-src 'self'; img-src 'self' https://image.tmdb.org; connect-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`.
- `docs/ambiente-dev.md` ganha a seção do front (subida com 1 comando além da API). `lib.md` atualizado antes do primeiro `npm install`.
- Nada de token/carrinho em `localStorage`/`sessionStorage`.

#### T2 — Cartaz e sessões do filme

- Rotas `/` (cartaz) e `/filmes/:id` (detalhe + sessões futuras) com TanStack Query; estados carregando/vazio/erro/404.
- Título e sinopse só como texto; pôster com `src` de `image.tmdb.org` (ou placeholder); **atribuição do TMDB visível**.
- Horários de UTC para `America/Sao_Paulo` e preço em BRL (`Intl`), testados; classificação indicativa com as cores oficiais, quando houver.
- Testes de componente sem rede (fetch falso).

#### T3 — Mapa de assentos isolado (dados fake)

- Componente puro `MapaDeAssentos({layout, ocupados, meus, selecionados, onAlternar})`, sem I/O; fixture com fileira irregular, vão e PCD.
- Acessibilidade: `role="grid"` com *roving tabindex* (Tab único entra na grade, setas movem, Enter/Espaço alternam), `aria-label` completo ("Fileira C, assento 7, PCD, ocupado"), `aria-pressed` no selecionado, **estado distinguível sem cor** (forma/ícone), foco visível em tungstênio.
- Limite de 6 selecionados na UI (a regra do servidor segue valendo); ocupados e vãos não selecionáveis.
- Testes de componente com teclado (user-event) obrigatórios.

#### T4 — Integração com a trava real + E2E do M3

- Container da sessão: `mapa` 1× + `ocupacao` com `refetchInterval` de 3–5 s, **pausado com a aba oculta**, cancelado ao sair da rota.
- Travar selecionados → `POST /api/sessoes/{id}/holds`; a UI **não assume sucesso** antes da resposta; **409 invalida a ocupação na hora** e mostra quais assentos falharam; `limite_holds` e 429 com mensagem clara.
- Painel "meus assentos" (`GET /holds`), contagem regressiva por `expira_em` (aviso ≤ 60 s), extensão única (botão desabilitado depois), liberar.
- Testes de integração com **fake timers** (intervalo, pausa, parada ao desmontar, 409 fora do ciclo, contagem com relógio falso).
- **Playwright** (Page Object Model, ADR 0006) com **dois contextos**: A trava, B vê ocupado no polling, B força → 409 tratado; hold liberado volta a ficar livre para B.
- **Job de CI próprio** (dispara com `web/**`, `internal/reserva/**`, `internal/sessao/**`): sobe a stack com o profile `app` do compose (migrations no boot), aguarda `/health`, sobe a SPA com proxy e roda o Playwright.

### Exigências transferidas

- **E0c-CD**: Caddyfile com `handle_path /api/*` → API, estáticos do `web/dist` com `try_files {path} /index.html`, headers de segurança da T1; E2E do M3 contra a URL pública no smoke.
- **E8**: login/conta no SPA, access token só em memória, single-flight do refresh.
- **E9**: todas as telas do operador (salas, sessões, filmes, pedidos).
- **E12**: single-flight/anti-stampede do cartaz, decidido pela métrica hit/miss sob k6.

### Perguntas escaladas ao usuário (respondidas em 2026-09-29)

1. ADR "Frontend SPA" → **autorizado (ADR 0009)**.
2. Telas do operador → **todas no E9** (roadmap ajustado).
3. E2E do M3 → **no CI, job próprio**.
4. Headers de segurança → **definidos no PRD da T1, aplicados no E0c-CD**.
