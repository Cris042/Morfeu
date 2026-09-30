# PRD 0035 — SPA: consulta e ingresso + E2E do M4 (E8, T5)

- **Task:** docs/tasks/0035-spa-consulta-m4.md
- **Branch:** feature/0035-spa-consulta-m4
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Fechar o E8 e provar o **M4 — compra completa ponta a ponta** (cartaz → assento → pagamento → ingresso no e-mail) no navegador, sem rede externa. Fontes: `docs/refinamentos/E8-spa-checkout.md` §T5 (decisões do usuário: rota `__teste` só com fake, página `/i/*` pela SPA, `@axe-core/playwright`), PRDs 0031–0034.

## Escopo

Telas, rota de teste do e-mail, E2E, CSP no preview, snippet do Caddy.

## Fora de escopo

- Stripe real no E2E (o Payment Element fica validado por mocks na 0033 e por smoke manual no deploy — E0c-CD).
- Aplicar o Caddy (E0c-CD, bloqueado pela VM).

## Requisitos funcionais

- RF01 — `/consulta`: e-mail + código (aceita minúsculas/hífens — a API normaliza) → pedido (código, situação, assentos, total) e links "Ingresso {assento}" para `/i/{ref}`; sem ingressos → aviso de que aparecem após a confirmação. Erros: 404 → **mensagem única** (não diz qual campo errou); 429; genérica.
- RF02 — `/i/:ref`: `GET /api/i/{ref}` → filme, sessão (dia + hora no fuso do cinema), sala, assento e QR (`<img src="/api/i/{ref}/qr.png" referrerpolicy="no-referrer">`); `usado` → aviso; 404 → "link inválido"; 410 → "não vale mais"; ambos com link para a consulta. Sem scripts de terceiros.
- RF03 — Cliente HTTP com `referrerPolicy: 'no-referrer'` em toda chamada (a API nunca precisa do Referer; o token está no path da página).
- RF04 — Links "Consultar pedido" no menu (visitante) e no "pedido não encontrado neste navegador".
- RF05 — `GET /__teste/emails?para=` (main): lista o que o **fake** do e-mail "enviou" ao destinatário (tipo, assunto e caminhos `/i/…`). Registrada só com e-mail fake **e** `-mode=all` **e** as travas da rota de pagamento de teste (gateway fake + segredo do webhook; o boot recusa fakes em produção). Sem `para` → lista vazia.
- RF06 — E2E `web/e2e/m4.spec.ts` (M3 intacto):
  - **convidado**: cartaz → filme → sessão → D3 → checkout → "Pagar (teste)" → "Pagamento confirmado" → link do e-mail (fake) → página com QR carregado → consulta por e-mail em maiúsculas + código em minúsculas → link do mesmo ingresso → assento ocupado para outro navegador;
  - **conta**: registro + login com volta → D4 → e-mail da conta pré-preenchido → pago → "Meus pedidos" com o pedido "Pago" → 1 e-mail;
  - `@axe-core/playwright` (WCAG 2.0/2.1 A/AA) no cartaz, assentos, checkout, ingresso e consulta — relatório com seletor e motivo;
  - `securitypolicyviolation` coletado por init script → qualquer violação falha;
  - sem sleep fixo (esperas por `expect`/`expect.poll`); sem rede externa.
- RF07 — CSP de produção no `vite preview` (cópia literal da de `configs/caddy/seguranca.caddy`); o `web-ci` falha se as duas divergirem e passa a rodar quando `configs/caddy/**` muda.
- RF08 — Snippet `(ingresso)` do Caddy: `/i/*` da SPA com `no-referrer` + `no-store`; instrução de redação do access log de `/i/*` e `/api/i/*` para a E0c-CD.

## Requisitos não funcionais

- RNF01 — A11y: formulários rotulados, erro em `role="alert"`, QR com texto alternativo; contraste AA (o axe achou o rodapé do TMDB em 4,08:1 → token `--nevoa`).
- RNF02 — A ref do ingresso só em memória/URL da página (nunca storage).

## Regras de negócio

- RN01 — Nenhuma tela revela se um e-mail tem pedido.

## Critérios de aceite

- [ ] CA01 — Consulta: acerto com links e corpo exato; pendente sem links; 404/429/500 com mensagens próprias; storage vazio.
- [ ] CA02 — Ingresso: válido com QR `no-referrer` e fetch `no-referrer`; usado; 404/410 sem QR e com link da consulta.
- [ ] CA03 — Rota de teste do e-mail: só o destinatário pedido; sem `para` → vazio; travas conferidas (unit).
- [ ] CA04 — E2E M4 convidado e conta verdes, sem violação de CSP nem do axe; M3 e sessão verdes.
- [ ] CA05 — Gate de CSP igual; lint, typecheck, testes, build e CI (web-ci, CI Go, E2E) verdes.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Consulta e ingresso com `apiFalsa` | componente | CA01–CA02 |
| `emailsParaTeste`, `rotasDeTesteAtivas` | unit Go (`cmd/morfeu`) | CA03 |
| Jornadas M4 na stack real (compose) | E2E Playwright + axe + CSP | CA04 |
| Igualdade da CSP | CI (`web-ci`) | CA05 |

## Plano de implementação

1. Rota de teste do e-mail (main) + unit.
2. `api/consulta.ts`, telas, rotas, links, `no-referrer`.
3. `@axe-core/playwright` (Context7 + npm audit + `lib.md`); CSP no preview + gate; snippet do Caddy.
4. `m4.spec.ts`; docs; roadmap (E8 e M4).

**Skills de apoio (§4.4):** `e2e-testing`, `frontend-patterns`.

## Arquivos que serão criados

- `web/src/api/consulta.ts`, `web/src/features/consulta/{Consulta.tsx, Ingresso.tsx, Consulta.module.css, Consulta.test.tsx}`
- `web/e2e/m4.spec.ts`, `cmd/morfeu/rotas_teste_test.go`
- `docs/tasks/0035-spa-consulta-m4.md`, `docs/prd/0035-spa-consulta-m4.md`

## Arquivos que serão modificados

- `web/src/api/{client.ts, client.test.ts}`, `web/src/app/{App.tsx, App.module.css, MenuConta.tsx}`, `web/src/features/checkout/AcompanharPedido.tsx`, `web/src/features/conta/MeusPedidos.tsx` (`ROTULOS` exportado)
- `web/{vite.config.ts, package.json, package-lock.json}`, `cmd/morfeu/main.go`
- `configs/caddy/seguranca.caddy`, `.github/workflows/web-ci.yml`
- `lib.md`, `docs/roadmap.md`, `docs/ambiente-dev.md`, `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 30.

## Dependências utilizadas

`@axe-core/playwright` 4.13.0 (dev; traz `axe-core` ~4.13); npm audit sem vulnerabilidades em 2026-09-30; uso conferido na doc do Playwright 1.63 (Context7).

## Impactos técnicos

- Job E2E +~10 s (duas jornadas + axe).
- O `vite preview` passa a servir com a CSP de produção (o dev server não — HMR usa inline).

## Riscos

- Regra do axe que dependa de conteúdo externo (pôster do TMDB) → o E2E roda sem TMDB (sem pôster); revisar quando houver.
- Execução local repetida sem reseed falha (assentos já vendidos) — documentado no spec e no `ambiente-dev.md`.

## Estratégia de rollback

Reverter o merge: somem as telas e o E2E do M4; a rota de teste do e-mail deixa de existir.
