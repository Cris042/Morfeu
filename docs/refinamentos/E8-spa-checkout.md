# Refinamento — Épico E8 (SPA checkout + convidado/conta) — 2026-09-30

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate → 4 perguntas escaladas e **respondidas pelo usuário no mesmo dia**. **Sem ADR**: o modelo de sessão do SPA é consequência do E1; o pagamento embutido, do ADR 0010; a política da rota de teste cabe no PRD. Fecha o **M4**.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | 5 tasks | polling (não SSE); Web Locks + BroadcastChannel p/ single-flight entre abas; `POST /pedidos` com Bearer opcional (`usuario_id` só do JWT; inválido → 401); retomada do 409 pelo `retrieve` no gateway (nunca persistir o `client_secret`); Stripe via `@stripe/stripe-js` + `@stripe/react-stripe-js`; QR PNG no backend; rota de teste só com gateway fake |
| security | 4 tasks | CSP mínima para o Stripe (sem `unsafe-*`); `client_secret` só em memória; consulta de convidado com resposta única + comparação constante + rate limit por IP e por e-mail; `/i/*`: parse estrito, HMAC sempre calculado, `hmac.Equal`, **410 só após HMAC válido**, headers, path redigido; preferia pagamento fake em processo (sem rota HTTP) |
| qa | 4 tasks | `refreshUnico` puro e testável (concorrência/falha/retry) + Playwright com 2 páginas contando `POST /auth/refresh`; polling com timers falsos; indistinguibilidade byte a byte; 404 × 410 sem oráculo; axe nas 3 telas; E2E do M3 intacto; bundle de produção sem fake (grep) |
| sre-devops | 4 tasks | `VITE_STRIPE_PK` e `VITE_PAGAMENTO_MODO` no build (ramo fake eliminado); CSP única com Stripe + matcher `/i/*` com `no-referrer`/`no-store`; `loadStripe` só na rota lazy; `securitypolicyviolation` falha o E2E; sem métricas no front; path `/i/*` redigido no access log; `/i/*` idempotente (scanners de e-mail) |
| backend-dev | 5 tasks | `autenticacao.Opcional` + `UsuarioDe` injetado (o `pedido` não importa `autenticacao`); `Gateway.RecuperarSegredo` (Stripe `Retrieve`; fake determinístico); `@stripe/react-stripe-js` 6.12.0 (peer React < 20) + `@stripe/stripe-js` 9.17.0; T1 e T4 paralelizáveis |

## Debate (divergências e resolução)

1. **Fake no SPA** — flag de build × prefixo do `client_secret`. → **Consenso** (QA, SRE, backend): **flag de build `VITE_PAGAMENTO_MODO`** com import dinâmico — o bundle de produção não carrega nada fake (grep no CI); o prefixo deixaria um caminho fake vivo em produção.
2. **Publishable key** → **Consenso** (SRE, backend): `VITE_STRIPE_PK` no build (chave pública; rebuild ao trocar).
3. **ADR** → **Consenso**: nenhum (ver introdução).
4. **Como "pagar" no E2E** → **Escalado**: usuário escolheu a **rota `POST /__teste/pagar/{pedido}`**, registrada só com gateway fake, montando o webhook assinado e chamando o handler real; teste de ausência com o Stripe; o boot já recusa fake em produção (0024).
5. **"Meus pedidos"** → **Escalado**: usuário escolheu **incluir no E8**.
6. **Página `/i/*`** → **Escalado**: usuário escolheu **servida pela SPA** (API devolve JSON + PNG do QR; headers por matcher `/i/*` no Caddy).
7. **axe** → **Escalado**: usuário aprovou **`@axe-core/playwright`** (dev dep, se `npm audit` limpo).
8. **Quebra** → **Consenso**: **5 tasks** (cada uma ≤ 30 arquivos; T1 e T4 independentes).

## Conclusão

- 5 tasks (0031–0035). Deps novas: `@stripe/stripe-js`, `@stripe/react-stripe-js` (runtime, só na rota do checkout) e `@axe-core/playwright` (dev) — Context7 + `npm audit` + `lib.md` antes de usar. Nenhuma dep nova no backend.
- Riscos: (1) indistinguibilidade da consulta e do `/i/*` (enumeração); (2) rota de teste existir em produção — impossível por construção + teste; (3) CSP com Stripe não exercitada no CI (sem rede) — risco residual até o deploy (E0c-CD), mitigado por teste da config e smoke manual com chave de teste; (4) single-flight do refresh entre abas.

## Exigências por task

### T1 (0031) — Backend do checkout e da conta
- `autenticacao.Opcional`: sem header segue convidado; header presente e inválido → 401 (nunca cai para convidado); válido → `sub` no contexto; `pedido` recebe `UsuarioDe` injetado no main (sem importar `autenticacao`).
- `POST /pedidos`: `usuario_id` só do JWT (nunca do corpo); pedido de convidado com `usuario_id` nulo; sem adoção retroativa.
- `Gateway.RecuperarSegredo` (Stripe `PaymentIntents.Retrieve` pelo `chamar`; fake determinístico); `GET /pedidos/{id}/pagamento` só para o dono do carrinho, só `aguardando_pagamento` no prazo, `Cache-Control: no-store`, nunca persistido nem logado.
- **Meus pedidos**: `GET /pedidos` (JWT obrigatório, filtra por `usuario_id`, paginado) e `GET /pedidos/{id}` também para o dono pela conta; pedido alheio → 404.
- `POST /__teste/pagar/{pedido}`: registrada **só** com gateway fake; monta `payment_intent.succeeded` assinado com o segredo do webhook e chama o handler real; teste de ausência com gateway Stripe; documentada em `ambiente-dev.md`.
- Testes: vínculo (logado × convidado × Bearer inválido), retomada (dono × alheio × expirado × pago), meus pedidos (só os seus), rota de teste presente/ausente.

### T2 (0032) — SPA: sessão, login/cadastro e "Meus pedidos"
- Access token em variável de módulo (nunca storage — assert no teste); `refreshUnico` puro com dependências injetadas: promessa única na aba + Web Locks entre abas, relendo o estado dentro do lock; BroadcastChannel só com resultado/logout; 401 do refresh → deslogado sem loop; fallback sem Web Locks = single-flight na aba.
- Cliente HTTP: Bearer quando logado; 401 → refresh + 1 retry; `X-Requested-With` sempre em escrita.
- Telas de login, cadastro, logout e "Meus pedidos" (lista + detalhe); e-mail pré-preenchido no checkout quando logado.
- Testes: concorrência (N chamadas → 1 refresh), falha propagada e retry posterior, contrato do lock entre "abas", sessão restaurada no boot, logout propagado; Playwright com 2 páginas contando `POST /auth/refresh` (família não revogada).

### T3 (0033) — SPA: checkout e pagamento
- Deps Stripe fixadas (Context7 + `npm audit` + `lib.md`); `loadStripe` só na rota lazy do checkout; `VITE_STRIPE_PK`; `VITE_PAGAMENTO_MODO=fake` → componente "Pagar (teste)" (chama a rota de teste) em chunk eliminado do bundle de produção.
- Fluxo: e-mail (ou da conta) → `POST /pedidos` → Payment Element com `client_secret` (só em memória) → polling de `GET /pedidos/{id}` com backoff até estado terminal (pausa sem foco, teto com "estamos confirmando, você receberá o e-mail") → telas de pago/expirado/falhou/estorno; 409 `pedido_pendente` → retomada; `holds_invalidos` → volta ao mapa; 503/429 com mensagens próprias.
- CSP do Caddy: `script-src 'self' https://js.stripe.com`, `frame-src https://js.stripe.com https://hooks.stripe.com`, `connect-src 'self' https://api.stripe.com`; sem `unsafe-*`; sem `style={{}}`.
- A11y: rótulos, erros em `role="alert"`, status em `aria-live`, teclado até pagar. Testes com timers falsos para o polling e todos os estados.

### T4 (0034) — Backend: consulta de convidado e ingresso
- `POST /pedidos/consulta {email, codigo}`: resposta idêntica (status/corpo/headers) para todo "não encontrado"; comparação constante executando sempre o mesmo caminho; rate limit por IP e por e-mail normalizado (baixo); devolve pedido + links dos ingressos (nunca `client_secret`); corpo só em POST.
- `GET /i/{id}.{token}` e `GET /i/{id}.{token}/qr.png`: parse estrito; HMAC com o segredo da versão (calculado mesmo p/ id inexistente) + `hmac.Equal`; 404 idêntico; **410 só após HMAC válido** (início da sessão + 24 h; ingresso cancelado); `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `nosniff`; idempotente; rate limit por IP; path redigido no access log (API e Caddy) e métricas pelo template da rota.
- Testes: tabela de casos byte a byte (consulta e `/i/*`), 404 × 410 sem oráculo, versão antiga do token, rate limits, headers.

### T5 (0035) — SPA: consulta e ingresso + E2E do M4
- Telas de consulta de convidado (mensagem única de erro) e do ingresso (`/i/:param` → API; estados válido/404/410/revogado; sem scripts de terceiros; `referrerPolicy: 'no-referrer'` no fetch); fallback de SPA para `/i/*` no Caddy com headers próprios.
- E2E do M4 (arquivo novo; M3 intacto): cartaz → assento → checkout (fake) → "Pagar (teste)" → polling → pago → e-mail (fake verificável) → link `/i/…` com QR visível; convidado → consulta por e-mail + código; conta → "Meus pedidos"; axe nas telas de assentos, checkout e ingresso; `securitypolicyviolation` falha o teste; grep do bundle de produção sem fake nem `sk_`/`whsec_`; sem rede externa; sem sleep fixo.

### Perguntas escaladas ao usuário (respondidas em 2026-09-30)

1. Pagar no E2E → **rota `__teste/pagar` só com gateway fake**.
2. "Meus pedidos" → **incluir no E8**.
3. Página `/i/*` → **pela SPA**.
4. `@axe-core/playwright` → **sim**.
