# PRD 0033 — SPA: checkout e pagamento (E8, T3)

- **Task:** docs/tasks/0033-spa-checkout.md
- **Branch:** feature/0033-spa-checkout
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Fechar o caminho de compra no SPA sobre a API da 0023–0031: do painel "Seus assentos" ao pagamento e ao resultado assíncrono do webhook. Fontes: `docs/refinamentos/E8-spa-checkout.md` §T3, ADR 0009 (SPA/CSP) e ADR 0010 (saga).

## Escopo

Checkout, meios de pagamento, acompanhamento, CSP, PaymentIntent sem redirecionamento, gates de bundle, deps.

## Fora de escopo

- Consulta de convidado, página do ingresso (0034/0035).
- E2E do M4 com axe e checagem de CSP no navegador (0035). Esta task deixa o build do E2E em modo fake e a rota de teste ligada no job E2E.

## Requisitos funcionais

- RF01 — **Entrada**: "Continuar para o pagamento" no painel "Seus assentos" → `/sessoes/:id/pagamento`. Assentos = holds vivos da sessão, em ordem; nenhum → "Nenhum assento reservado" + volta ao mapa.
- RF02 — **E-mail**: obrigatório; pré-preenchido com o da conta quando logado (editável; o pedido leva o Bearer e a API vincula a conta — 0031); convidado vê "Compra como convidado" + link para entrar com volta.
- RF03 — **Pedido**: `POST /pedidos {email, sessao_id, assentos}` →
  - 201 → fase de pagamento com código, total e "Pague até";
  - 409 `pedido_pendente` (com `pedido_id`) → `POST /pedidos/{id}/retomar` → fase de pagamento do pedido existente;
  - 409 `holds_invalidos` ou 404 na retomada → mensagem + "Escolher assentos" (holds reconsultados);
  - 400 → e-mail inválido; 503 `pagamento_indisponivel` → assentos seguem reservados; 429 → espere; resto → genérica.
- RF04 — **`client_secret`**: só em estado de componente — nunca em cache do TanStack Query, storage, URL ou log.
- RF05 — **Meio de pagamento — um por build** (`VITE_PAGAMENTO_MODO` resolvido no build):
  - `fake` → "Pagar (teste)" chama `POST /__teste/pagar/{id}` (a API assina o webhook pelo caminho real);
  - padrão → Payment Element (`@stripe/react-stripe-js`), `loadStripe(VITE_STRIPE_PK)` só no chunk lazy; `confirmPayment({elements, redirect: 'if_required'})`; erro do Stripe exibido; sem chave → aviso de indisponível.
  - O chunk do outro modo não é gerado.
- RF06 — **Sem redirecionamento**: o PaymentIntent passa a ser criado com `automatic_payment_methods[allow_redirects]=never` — a confirmação termina na página e não existe URL de retorno com o segredo.
- RF07 — **Acompanhamento** `/pedido/:id`: `GET /pedidos/{id}` (carrinho ou conta) com polling 1 s, 1 s, 2 s, 4 s e depois 5 s; pausa com a aba oculta; para em `pago|expirado|falhou|estornado`; `estorno_pendente` segue consultando; teto de 2 min → "Estamos confirmando seu pagamento… você receberá o e-mail" + "Verificar de novo". Telas para cada estado; `pago` invalida os holds; 404 → "não encontrado neste navegador".
- RF08 — **CSP** (`configs/caddy/seguranca.caddy`, aplicada no E0c-CD): `script-src 'self' https://js.stripe.com https://*.js.stripe.com`, `frame-src https://js.stripe.com https://*.js.stripe.com https://hooks.stripe.com`, `connect-src 'self' https://api.stripe.com`; nada de `unsafe-*`.
- RF09 — **Gates**: `web-ci` falha se o bundle de produção contiver `__teste`/"Pagar (teste)" ou `sk_`/`rk_`/`whsec_`; o E2E builda com `VITE_PAGAMENTO_MODO=fake` e o job E2E define `STRIPE_WEBHOOK_SECRET` aleatório (rota de teste ativa).

## Requisitos não funcionais

- RNF01 — A11y: campos rotulados, erros em `role="alert"`, estado do pedido em `role="status"` `aria-live="polite"`, teclado até pagar.
- RNF02 — Sem `style={{}}` inline; aparência do Stripe pelos tokens via `appearance` (dentro do iframe).
- RNF03 — Deps novas fixadas (exatas), Context7 + `npm audit` + `lib.md`.

## Regras de negócio

- RN01 — O servidor calcula o total; o SPA nunca envia preço.
- RN02 — Pagamento que chega depois do prazo é estornado pela saga; o SPA só informa.

## Critérios de aceite

- [ ] CA01 — Convidado: só os assentos da sessão, corpo exato do pedido, fase de pagamento com código/total, segredo fora do DOM e do storage.
- [ ] CA02 — Logado: e-mail pré-preenchido e Bearer no `POST /pedidos`.
- [ ] CA03 — Pendente → retomada; retomada 404 → volta ao mapa; 409/400/503/429/500 com mensagens próprias; sem holds → tela própria.
- [ ] CA04 — Fake: chama a rota de teste com anti-CSRF e conclui; falha permite tentar de novo. Stripe: `confirmPayment` sem redirecionamento; recusa exibida; 2ª tentativa conclui.
- [ ] CA05 — Polling: backoff, parada no terminal, teto + "Verificar de novo", telas de expirado/falhou/estorno, 404.
- [ ] CA06 — `allow_redirects=never` enviado ao Stripe (teste do adapter).
- [ ] CA07 — Build de produção sem chunk de teste e sem segredos; build fake sem Stripe.js; lint, typecheck, testes e CI verdes.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Checkout pelo `App` com `apiFalsa` | componente | CA01–CA03 |
| Meios com `vi.stubEnv` + mocks do Stripe | componente | CA04 |
| Acompanhamento com timers falsos | componente | CA05 |
| Formulário enviado ao Stripe | unit Go (`pagamento`) | CA06 |
| Grep do bundle | CI (`web-ci`) | CA07 |
| Fluxo assento → checkout → "Pagar (teste)" → pago (smoke manual na stack local; o E2E versionado é da 0035) | manual | RF05/RF07 |

## Plano de implementação

1. Deps + `lib.md`; `allow_redirects=never`.
2. `api/checkout.ts`, telas e meios; rotas e link do painel.
3. Testes; CSP; gates; docs.

**Skills de apoio (§4.4):** `frontend-patterns`, `security-review`.

## Arquivos que serão criados

- `web/src/api/checkout.ts`, `web/src/vite-env.d.ts`
- `web/src/features/checkout/{Checkout.tsx, AcompanharPedido.tsx, Pagamento.tsx, PagamentoStripe.tsx, PagarTeste.tsx, tipos.ts, Checkout.module.css, Checkout.test.tsx, Pagamento.test.tsx, AcompanharPedido.test.tsx}`
- `docs/tasks/0033-spa-checkout.md`, `docs/prd/0033-spa-checkout.md`

## Arquivos que serão modificados

- `web/{package.json, package-lock.json, playwright.config.ts}`, `web/src/app/App.tsx`, `web/src/features/sessao/{SeusAssentos.tsx, PaginaSessao.module.css}`
- `internal/pedido/pagamento/{stripe.go, stripe_test.go}`
- `configs/caddy/seguranca.caddy`, `.github/workflows/{web-ci.yml, e2e.yml}`
- `lib.md`, `docs/ambiente-dev.md`, `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 30 (no teto — §6.3).

## Dependências utilizadas

`@stripe/stripe-js` 9.17.0 e `@stripe/react-stripe-js` 6.12.0 (peer `react <20`, `@stripe/stripe-js >=9.16 <10`); `npm audit` sem vulnerabilidades em 2026-09-30; API conferida no Context7 (`/stripe/react-stripe-js`).

## Impactos técnicos

- Chunk `PagamentoStripe` (~11 KB) + Stripe.js de `js.stripe.com` só na fase de pagamento.
- Pagamentos por métodos de redirecionamento ficam indisponíveis (cartão e carteiras sem redirecionamento seguem).

## Riscos

- CSP incompleta para o Stripe em produção → validação no E0c-CD com o Caddy real.
- Stripe.js falha ao carregar → botão fica desabilitado (sem instância); a reserva segue até o prazo.

## Estratégia de rollback

Reverter o merge: o SPA volta a parar no "Seus assentos"; o PaymentIntent volta a aceitar redirecionamento; nenhuma migration.
