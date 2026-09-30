# ADR 0009 — Frontend SPA: React + TypeScript + Vite, fronteira `/api` por proxy e estáticos pelo Caddy

- **Status:** aceito
- **Data:** 2026-09-29
- **Task/PRD relacionados:** refinamento E5 (`docs/refinamentos/E5-spa-cliente.md`), tasks 0017–0020; `doc.md` §4 (mesmo domínio via Caddy)

## Contexto

O E5 cria o primeiro código de frontend do projeto. A descoberta fixou "React + Vite, SPA sem SSR" e "SPA e API sob o mesmo domínio via Caddy" (sem CORS permissivo, cookies simples), mas deixou abertos:
- a estrutura do front;
- as bibliotecas de dados e estilo;
- como separar os caminhos da SPA dos da API.

A última questão é concreta: a SPA quer URLs como `/filmes/12`, e a API já usa `/filmes/12`. As rotas do Echo não têm prefixo, e quatro módulos (E1–E4) têm suítes de integração escritas sobre esses caminhos. A decisão vale para o E5, o E8 e o E9, e amarra o deploy (E0c-CD).

## Escopo

Cobre:
- stack e estrutura do `web/`;
- contrato de rede do front (cliente HTTP único);
- fronteira de caminhos SPA × API;
- como a SPA é servida em dev, CI e produção.

Não cobre:
- telas específicas (PRDs);
- autenticação no SPA (E8);
- política de cache HTTP dos estáticos (E0c-CD).

## Decisão

**SPA em React + TypeScript estrito + Vite, em `web/`. O navegador chama a API sempre em `/api/*`, e o proxy da borda remove o prefixo antes de chegar ao Echo, que continua sem prefixo. Em produção, o Caddy serve os estáticos. Em dev e no CI, rodam dois processos (API + Vite com proxy).**

- **Dados:** TanStack Query.
  - Cuida do polling com `refetchInterval`, que pausa com a aba oculta.
  - Cancela requisições ao sair da rota e invalida a ocupação após 409 ou após travar.
  - Um único `src/api/client.ts` concentra `credentials: 'include'`, o header `X-Requested-With: morfeu` nas escritas e os erros tipados. Nenhum componente chama `fetch` direto.
- **Rotas:** React Router, em modo biblioteca.
- **Estilo:** CSS Modules sobre `tokens.css` (os 7 tokens da identidade visual), com fontes woff2 self-hosted.
- **Testes:**
  - Vitest + Testing Library + user-event para unidade e componente;
  - Playwright só para as jornadas críticas (ADR 0006).
- **Ferramental:** npm com lockfile versionado e Node LTS fixado.
- **Fronteira de caminhos:**
  - em dev, `server.proxy['/api']` do Vite com `rewrite`;
  - em prod, `handle_path /api/*` no Caddy e o resto com `try_files {path} /index.html`.
- **CI:** workflow próprio do front (lint, typecheck, test, build, `npm audit` como gate) e um job de E2E que sobe a stack pelo compose.

## Tecnologias ou padrões envolvidos

React, TypeScript, Vite, TanStack Query, React Router, CSS Modules, Vitest, Testing Library, Playwright, npm, Caddy (reverse proxy e estáticos), padrão *container/presentational* no mapa de assentos.

## Impacto esperado

- **Código:** nasce `web/`, uma árvore Node independente do módulo Go. O backend não muda: nenhuma rota, teste ou imagem Docker é tocada.
- **CI:** ganha um workflow do front e, na T4, um job de E2E.
- **Deploy (E0c-CD):** o Caddyfile precisa do `handle_path /api/*`, dos estáticos e dos headers de segurança definidos no PRD da T1.
- **Manutenção:** passa a haver dois ecossistemas de dependência (Go e npm), ambos auditados no CI e registrados no `lib.md`.

## Alternativas consideradas e descartadas

- **Grupo `/api` no Echo** — descartada: reescreve rotas e testes de quatro módulos já fechados, sem ganho funcional. A regra "igual em dev e prod" também vale com o strip nos dois proxies.
- **SPA embutida no binário (`go:embed`)** — descartada: acopla o build Go ao Node e muda a imagem ARM64 estável desde a task 0004. Continua como rota se um dia não houver Caddy.
- **Rotas da SPA disjuntas das da API** (ex.: `/cartaz`, `/sessao`) — descartada: frágil, porque cada rota nova da API pode colidir.
- **Hash router** — descartada: URLs ruins para um portfólio, e não resolve a fronteira no deploy.
- **Next.js/SSR** — descartada na descoberta: SSR desnecessário e um servidor Node a mais.
- **Tailwind / CSS-in-JS** — descartadas: duplicariam os tokens que já existem em CSS. O CSS-in-JS ainda tem custo de runtime e atrito com a CSP.
- **`fetch` + hook próprio de polling** — descartada: reimplementaria pausa, cancelamento, dedup e invalidação que o TanStack Query já traz testados.
- **TanStack Router** — descartada: a segurança de tipos nas rotas não paga o conceito extra com 3–4 rotas.

## Benefícios

- Backend e testes intocados; o front evolui sem risco para o E1–E4.
- Mesma origem em dev e prod: cookies `SameSite=Strict` funcionam e não existe CORS.
- A lógica de rede fica num único ponto testável (headers anti-CSRF, credentials, erros).
- Polling do M3 com pausa e cancelamento prontos, e custo de servidor absorvido pelo cache de 3 s (ADR 0008).

## Trade-offs

- O strip do prefixo é convenção de infraestrutura e fica invisível no Go. Um proxy mal configurado quebra tudo.
- Três dependências de runtime novas (react, react-router, @tanstack/react-query) mais o ferramental de build/teste, todas para manter atualizadas.
- Dois processos em dev, em vez de um.
- Sem VM, a SPA não é "servida em produção" até o E0c-CD.

## Riscos

- **Proxy de dev e prod divergirem** (probabilidade média, impacto alto): as rotas funcionam no Vite e falham no Caddy.
- **Supply chain npm** (probabilidade média, impacto médio): árvore transitiva grande.
- **CI mais lento** (probabilidade alta, impacto baixo): E2E +3–5 min.

## Estratégias para minimizar os trade-offs

- O E2E do M3 roda pelo proxy (caminho `/api` real), e o smoke do E0c-CD repete a jornada contra o Caddy.
- `npm ci` com lockfile, `npm audit --audit-level=high` como gate, cada dependência no `lib.md` com Context7/OSV, e nenhuma dependência de conveniência (fontes como arquivos, sem @fontsource).
- O job de E2E só dispara quando `web/`, a reserva ou as sessões mudam; o workflow do front roda em paralelo ao Go.

## ADRs relacionados

ADR 0001 (binário Go único, que segue sem servir estáticos), ADR 0003 (fronteiras: o front é mais um cliente da API pública), ADR 0006 (Playwright no topo da pirâmide), ADR 0008 (ocupação com cache de 3 s, pensada para este polling). Nenhum é substituído.

## Condições de reversão

Revisitar se:
- o deploy não puder usar Caddy (voltar a avaliar o `go:embed`);
- a SPA precisar de SEO/SSR real (improvável para venda de ingressos com sessão efêmera);
- o custo de manter o ecossistema npm superar o valor (reavaliar htmx + templates Go para páginas simples, mantendo só o mapa como ilha React).
