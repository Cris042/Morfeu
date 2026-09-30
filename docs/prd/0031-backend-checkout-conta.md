# PRD 0031 — Backend do checkout e da conta (E8, T1)

- **Task:** docs/tasks/0031-backend-checkout-conta.md
- **Branch:** feature/0031-backend-checkout-conta
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Dar ao SPA do E8 o que falta na API:

- o vínculo do pedido com a conta;
- "Meus pedidos";
- a retomada do pagamento de um pedido pendente, sem persistir o segredo do cliente;
- a rota de pagamento de teste, só com o gateway fake, que o E2E do M4 usa para "pagar" sem o Stripe.

Fontes: `docs/refinamentos/E8-spa-checkout.md` §T1, ADR 0003, ADR 0010, decisão do usuário (rota `__teste/pagar`).

## Requisitos funcionais

- RF01 — `autenticacao.Opcional(emissor)`:
  - sem header `Authorization` → segue anônimo;
  - header presente e inválido (malformado, expirado, adulterado) → **401**. Nunca cai para anônimo em silêncio: o SPA renova e repete;
  - válido → usuário e papel no contexto, como no `Exigir`.
- RF02 — `POST /pedidos` com o `Opcional`: `usuario_id` do pedido vem **só** do JWT, pela leitura `UsuarioDe` injetada pelo main. O `pedido` não importa `autenticacao` (ADR 0003). O corpo não aceita o campo. Convidado → nulo. Não há adoção retroativa.
- RF03 — `GET /pedidos/{id}` com o `Opcional`: o dono é o carrinho **ou** a conta vinculada. Pedido alheio → 404, nunca 403. Resposta com `Cache-Control: no-store`.
- RF04 — `GET /pedidos?pagina=N` ("Meus pedidos"):
  - JWT obrigatório (`Exigir` cliente|operador);
  - só os da conta, mais recentes primeiro;
  - páginas de 20;
  - `no-store`;
  - índice parcial `(usuario_id, criado_em DESC) WHERE usuario_id IS NOT NULL` (migration 015).
- RF05 — `Gateway.RecuperarSegredo(intencao)`:
  - Stripe: `PaymentIntents.Retrieve` pelo `chamar`, com breaker e métricas;
  - fake: o mesmo segredo determinístico da criação.
- RF06 — `POST /pedidos/{id}/retomar` (cookie do carrinho + anti-CSRF):
  - só o carrinho dono;
  - só `aguardando_pagamento` dentro do prazo e com cobrança;
  - devolve código, total, prazo e `client_secret` **lido do gateway, nunca persistido nem logado**;
  - `no-store`.
  - Fora disso → 404. Gateway fora → 503.
  - É POST porque o cliente HTTP só manda o anti-CSRF em escritas, e o POST evita cache.
- RF07 — `POST /__teste/pagar/{id}` (decisão do usuário):
  - Registrada **só** quando o main tem gateway fake e segredo de webhook.
  - Monta um `payment_intent.succeeded` com a cobrança e o total do pedido, **assina com o segredo do webhook** e passa pelo mesmo `Verificar` + `ProcessarEvento` da rota real (pivô, outbox, e-mail).
  - Com o gateway Stripe a rota não existe. O boot já recusa gateway fake com `AMBIENTE=producao` (0024), então são duas travas.
  - Aviso no log de boot quando ativa.

## Requisitos não funcionais

- RNF01 — `client_secret` e JWT nunca em log. Logs só com `pedido_id`.
- RNF02 — Nenhuma dependência nova.

## Critérios de aceite

- [ ] CA01 — Logado → `usuario_id` = sub; convidado → nulo; Bearer inválido → 401 e nenhum pedido do carrinho.
- [ ] CA02 — "Meus pedidos": 2 da conta em ordem decrescente, `no-store`; sem login → 401; pedido pela conta sem o carrinho → 200; pedido de outra conta → 404.
- [ ] CA03 — Retomada: segredo do gateway, código, `no-store`; outro carrinho → 404; repetível; expirado → 404; pago → 404.
- [ ] CA04 — Rota de teste → 204 e pedido `pago` com ingresso, evento e holds convertidos; sem o registro → 404/405.
- [ ] CA05 — `Opcional`: sem header, válido, expirado, adulterado, Basic.
- [ ] CA06 — Stripe `RecuperarSegredo` pelo servidor falso.
- [ ] CA07 — CI verde.

## Arquivos que serão criados

- `migrations/015_pedidos_usuario.{up,down}.sql`, `internal/pedido/teste.go`, `internal/pedido/conta_test.go`
- `docs/tasks/0031-backend-checkout-conta.md`, `docs/prd/0031-backend-checkout-conta.md`

## Arquivos que serão modificados

- `internal/autenticacao/{middleware.go, autenticacao_test.go}`
- `internal/pedido/{queries.sql, pedido.go, repositorio.go, service.go, handler.go, handler_test.go}`, `internal/pedido/db/queries.sql.go` (gerado)
- `internal/pedido/pagamento/{gateway.go, fake.go, stripe.go, stripe_test.go, webhook.go}`
- `cmd/morfeu/main.go`, `sqlc.yaml`, `docs/ambiente-dev.md`
- `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 29.

## Dependências utilizadas

Nenhuma nova.

## Estratégia de rollback

Reverter o merge e aplicar `015_pedidos_usuario.down.sql`.
