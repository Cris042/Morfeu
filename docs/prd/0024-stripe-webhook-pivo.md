# PRD 0024 — Stripe + webhook assinado + pivô do checkout (E6, T3)

- **Task:** docs/tasks/0024-stripe-webhook-pivo.md
- **Branch:** feature/0024-stripe-webhook-pivo
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Implementar o pivô da saga (ADR 0010). O pagamento aprovado chega pelo webhook assinado do Stripe. Numa única TX curta, sem I/O externo, o pedido vira `pago`, os holds viram vendidos, os ingressos são emitidos e o evento `pedido.confirmado` vai para a outbox. Quando não dá para vender, o pedido vai para `estorno_pendente` com o motivo, e o estorno é executado pela task 0025. Fontes: `docs/refinamentos/E6-saga-checkout.md` §T3, ADR 0010, doc.md §14.1–14.2.

## Escopo

- Dependência `stripe-go/v86` (Context7 + OSV + `lib.md`).
- Adapter Stripe do `pagamento.Gateway` e verificador do webhook.
- Rota `POST /webhooks/stripe` e pivô com dedup, cruzamento e savepoint.
- Migration 012 (`stripe_eventos`).
- Config de gateway com recusas de boot, métricas `gateway_*`, Stripe CLI no compose e regra do gitleaks.

## Fora de escopo

- Executar o estorno, reconciliar pedidos presos, cancelar a cobrança de pedido vencido (0025).
- Consumidor de `pedido.confirmado` e a fila ligada ao evento: até a 0026 o evento é publicado sem fila (topic sem binding, descartado pelo broker, aceitável antes de produção). Métricas da saga e alertas também ficam para a 0026.
- Token HMAC do ingresso (E7, que o usa no QR): os ingressos nascem com `versao_token = 1` e nada em claro. CSP do Payment Element (E8).

## Requisitos funcionais

- RF01 — `POST /webhooks/stripe`, rota pública fora do anti-CSRF:
  - Corpo lido **bruto**, ≤ 64 KB (maior → 413).
  - Assinatura `Stripe-Signature` verificada com `webhook.ConstructEventWithOptions`, tolerância de 300 s, antes de qualquer parse.
  - Timestamp mais de 300 s no futuro também é recusado (checagem própria: o SDK só recusa o antigo).
  - Inválida → 400 `{"erro":"assinatura_invalida"}`, sem detalhe.
  - Teto por IP de 300/min (só contra DoS).
- RF02 — Eventos tratados:
  - `payment_intent.succeeded` → pivô.
  - `payment_intent.payment_failed` → só funil `pagamento_recusado`; o pedido segue aguardando, porque o cliente pode tentar outro cartão no mesmo PaymentIntent.
  - Demais → 2xx sem efeito.
  - A versão de API do evento é ignorada (`IgnoreAPIVersionMismatch`), com parse próprio só dos campos usados: id, amount, currency, metadata.
- RF03 — **Pivô**, numa TX: dedup em `stripe_eventos(event_id PK)` (evento repetido → 2xx sem efeito), `SELECT … FOR UPDATE` do pedido pelo `metadata.pedido_id` e então:
  - Pedido inexistente ou sem metadata → sem efeito, log de erro.
  - `payment_intent_id` gravado e diferente do evento → sem efeito, log de erro.
  - `payment_intent_id` nulo (cobrança órfã, auditoria 0023) → grava o do evento.
  - Status fora de `aguardando_pagamento`/`expirado` → sem efeito (já processado).
  - `amount` ≠ total ou `currency` ≠ `brl` → `estorno_pendente` com motivo `divergencia`.
  - `expirado` → `estorno_pendente` com motivo `tardio`.
  - `aguardando_pagamento` (mesmo se já passou de `expira_em`, pois o hold preso tem 2 min de margem): num **savepoint**, `reserva.ConverterDoPedido` e um ingresso por assento (`ON CONFLICT DO NOTHING`).
    - Conjunto convertido ≠ assentos do pedido, ou assento já com ingresso → desfaz só o savepoint e vai para `estorno_pendente` com motivo `emissao`. Nunca há venda parcial.
    - Senão, CAS → `pago`, com trilha, e `outbox.Enqueue("pedido.confirmado", {"pedido_id"})`.
- RF04 — 2xx só depois do commit. Falha de processamento → 500, e o Stripe reenvia.
- RF05 — Adapter Stripe (`CriarCobranca`):
  - PaymentIntent com `amount`, `currency`, `metadata.pedido_id`, `automatic_payment_methods.enabled` e `Idempotency-Key` do pedido.
  - Timeout HTTP de 5 s. O SDK repete falhas de rede com a mesma chave (até 3 tentativas); erro já respondido pela API só é repetido no lock timeout, por decisão do SDK.
- RF06 — Circuit breaker **só no gateway**, código próprio com relógio injetado:
  - 5 falhas transitórias seguidas (rede, 429, 5xx) → aberto 30 s.
  - Aberto, recusa na hora (→ 503 + `Retry-After` + assentos devolvidos, fluxo da 0023).
  - Meio-aberto: sucesso fecha e falha reabre.
  - 4xx não conta.
- RF07 — Métricas `gateway_requests_total{op,resultado}`, `gateway_duration_seconds{op}`, `gateway_breaker_state`; funil ganha `pago`, `pagamento_recusado`, `estorno_necessario`.
- RF08 — Config e boot (só quando serve HTTP):
  - `AMBIENTE` (dev|producao), `MORFEU_GATEWAY` (fake|stripe, padrão fake), `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`.
  - Recusa: gateway fake em produção, Stripe sem chave, qualquer chave que não seja `sk_test_`/`rk_test_`, valores desconhecidos.
  - Sem segredo do webhook, a rota fica desligada (log de aviso).
- RF09 — Dev: serviço `stripe-cli` (v1.52.1) no profile `stripe` do compose (`stripe listen --forward-to app:8080/webhooks/stripe`). `.env.example` documentado. Regra do gitleaks para `whsec_`, porque as regras default cobrem `sk_`/`rk_` e não `whsec_`.

## Requisitos não funcionais

- RNF01 — O SDK do Stripe só é importado em `internal/pedido/pagamento` (depguard `pedido-pagamento`). O `pedido` depende só dos tipos da borda.
- RNF02 — Nunca logar corpo do webhook, `Stripe-Signature`, chaves ou e-mail. Erros do Stripe são logados só com status/tipo/código. O logger do SDK fica desligado.
- RNF03 — Testes sem rede:
  - Assinatura gerada no teste (`webhook.GenerateTestSignedPayload`).
  - Adapter contra um servidor HTTP falso local.
  - Integração com PG real, reserva e sessão reais.

## Regras de negócio

- RN01 — O pivô é irreversível. De `pago` só se sai por cancelamento (E9).
- RN02 — Pagamento que não pode virar venda nunca é "esquecido": vira `estorno_pendente` com motivo, e o estorno é automático (0025).

## Critérios de aceite

- [ ] CA01 — Webhook aprovado: 200; `pago`; 2 ingressos ativos; 2 holds `convertido`; 1 `pedido.confirmado` com payload `{"pedido_id"}`; trilha `aguardando_pagamento>pago`; funil `pago`.
- [ ] CA02 — Ocupação lista os vendidos, e o assento vendido não é travável nem 24 h depois.
- [ ] CA03 — 10 entregas simultâneas do mesmo evento → todas 200, 1 efeito. Evento novo para pedido já pago → sem efeito.
- [ ] CA04 — Sem assinatura, corpo adulterado, outro segredo, timestamp antigo → 400 `assinatura_invalida` sem efeito. Corpo > 64 KB → 413. Unit: cabeçalho malformado, JSON reformatado, timestamp futuro.
- [ ] CA05 — Valor ou moeda divergente → `estorno_pendente`/`divergencia`, sem ingresso.
- [ ] CA06 — Pedido expirado (lazy) + webhook → `estorno_pendente`/`tardio`, trilha `expirado>estorno_pendente`.
- [ ] CA07 — Hold preso venceu e outro carrinho levou 1 dos 2 assentos → `estorno_pendente`/`emissao`, 0 ingressos, 0 convertidos, hold do outro intacto.
- [ ] CA08 — Corrida webhook × expiração lazy (10 rodadas) → sempre `pago` completo ou `estorno_pendente`/`tardio` sem ingresso.
- [ ] CA09 — Recusado, desconhecido, pedido inexistente, sem metadata, outra cobrança → 200 sem mudança; funil `pagamento_recusado`.
- [ ] CA10 — Cobrança órfã recuperada pelo metadata.
- [ ] CA11 — Adapter: requisição com valor, moeda, metadata, métodos automáticos e `Idempotency-Key`; queda de rede repetida com a mesma chave; 500 conta no breaker; 400 não repete nem conta; chave live/pk recusada.
- [ ] CA12 — Breaker: abre na 5ª falha, recusa aberto, meio-aberto reabre na falha e fecha no sucesso; transições informadas.
- [ ] CA13 — Config: recusas de boot sem vazar a chave.
- [ ] CA14 — CI verde; gitleaks detecta `whsec_`.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Verificação do webhook, adapter contra servidor falso, breaker | unit (`pagamento`) | CA04 (parte), CA11, CA12 |
| Config | unit | CA13 |
| Webhook pelas rotas reais, PG real, reserva/sessão reais | integração | CA01–CA10 |

## Plano de implementação

1. Context7 + OSV + `lib.md`; `go get`.
2. Migration 012 + queries + `sqlc generate`.
3. `pagamento/stripe.go` (adapter + breaker) e `pagamento/webhook.go`.
4. `pivo.go`, `marcarEstorno`, porta `ConverterDoPedido`, rota do webhook.
5. Config + `main` (gateway, webhook, métricas) + depguard + compose + `.env.example` + gitleaks.
6. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `golang-database`, `security-review`.

## Arquivos que serão criados

- `migrations/012_stripe_eventos.{up,down}.sql`
- `internal/pedido/pivo.go`, `internal/pedido/pivo_test.go`
- `internal/pedido/pagamento/stripe.go`, `internal/pedido/pagamento/webhook.go`, `internal/pedido/pagamento/stripe_test.go`
- `docs/tasks/0024-stripe-webhook-pivo.md`, `docs/prd/0024-stripe-webhook-pivo.md`

## Arquivos que serão modificados

- `internal/pedido/{queries.sql, repositorio.go, service.go, handler.go, handler_test.go}`, `internal/pedido/db/{models.go, queries.sql.go}` (gerados)
- `internal/config/config.go`, `internal/config/config_test.go`, `cmd/morfeu/main.go`
- `go.mod`, `go.sum`, `lib.md`, `sqlc.yaml`, `.golangci.yml`, `.gitleaks.toml`, `.env.example`, `docker-compose.yml`
- `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 30. O roadmap é atualizado na abertura da 0025.

## Dependências utilizadas

`github.com/stripe/stripe-go/v86` v86.4.2: oficial, sem dependências de runtime, nenhuma advisory no OSV em 2026-09-30.

## Impactos técnicos

- Rota pública nova; o boot pode falhar por config de pagamento inválida (intencional).
- Eventos `pedido.confirmado` publicados sem fila até a 0026.

## Riscos

- Webhook forjado → assinatura sobre o corpo bruto + cruzamento de valor + testes negativos.
- Mudança de formato do evento pelo Stripe → parse mínimo dos campos estáveis; um evento ilegível é recusado (400) e o Stripe reenvia.

## Estratégia de rollback

Reverter o merge e aplicar `012_stripe_eventos.down.sql`. Com `MORFEU_GATEWAY=fake` e sem segredo de webhook, o comportamento é o da 0023.
