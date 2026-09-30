# Refinamento — Épico E6 (Saga do checkout) — 2026-09-30

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate → 4 perguntas escaladas e **respondidas pelo usuário no mesmo dia**.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com ressalvas | pivô = CAS `aguardando_pagamento → pago` na mesma TX que emite ingressos; saga **síncrona no webhook**, sem Actor nem fila; ingressos no `pedido`; conversão hold→`convertido` por **porta síncrona que recebe a TX**; hold estendido até o fim do pedido; reconciliador no worker; sem `confirmado` (e-mail fora da máquina); PaymentIntent + Element; recomenda **ADR da saga**; Tempo no E10 |
| security | seguir com ressalvas | assinatura sobre o **corpo bruto** (≤ 64 KB, 300 s), dedup em tabela própria na TX, rota fora do anti-CSRF; cruzamento `amount`+`currency`+`metadata.pedido_id`; token do ingresso nunca em claro no banco; posse → 404, código não sequencial; 1 pedido pendente por carrinho + rate limit; `Idempotency-Key` em toda escrita no Stripe; segredos só por env, gitleaks para `sk_`/`rk_`/`whsec_`, boot recusa `sk_live_`; evento só com IDs; replay por CLI; preferia Checkout Session hospedado |
| qa | seguir com ressalvas | matriz estado×evento completa e independente; CAS concorrente → exatamente 1; corridas determinísticas (webhook × expiração nas duas ordens, 10 webhooks duplicados, roubo do hold durante o pagamento, reconciliação × webhook tardio); fake programável único (CI + load-test); webhook sem rede com assinatura gerada no teste; E2E Playwright só no E8 — no E6, teste de integração de API ponta a ponta; regressões do E4 (sweeper/upsert/ocupação com `convertido`) |
| sre-devops | seguir com ressalvas | Stripe CLI no compose com `profiles: ["stripe"]`; CI sem rede; timeout 5 s, retry só rede/5xx com jitter, breaker só no gateway (503 + `Retry-After` + libera holds); reconciliação e estorno como **jobs periódicos no worker** (lote limitado); métricas `gateway_*`, `saga_*`, `pedidos_presos`; alertas versionados; Tempo no E10 (spans já instrumentados); replay `-mode=replay` com dry-run/limite; shutdown gracioso, prefetch 10, health do worker; limpeza da outbox publicada |
| backend-dev | seguir com ressalvas | `outbox.Tx`/`WithTx` já existem → porta transacional viável sem quebrar o ADR 0003; **achado crítico: `convertido` sai do índice único parcial `WHERE status='ativo'`** — um novo hold poderia ser criado sobre assento vendido e a ocupação o mostraria livre → migration com `status IN ('ativo','convertido')`, roubo só de `ativo` vencido, ocupação conta `convertido`; breaker próprio (~40 linhas); `PrenderParaPedido` sem consumir a extensão; quebra em **6 tasks** |

## Debate (divergências e resolução)

1. **Estados** — `confirmado` (QA), `divergente`/`estornando` (security), `estorno_pendente` (SRE/backend). → **Consenso**: `aguardando_pagamento`, `pago` (pivô), `expirado`, `falhou`, `estorno_pendente`, `estornado`, `cancelado` (E9). Sem `confirmado` (e-mail pós-pivô fica fora da máquina — `doc.md` §10); divergência e estorno em curso viram **colunas** (`motivo_estorno`, `tentativas_estorno`) e a `Idempotency-Key` cobre a concorrência do estorno (§1 anti-overengineering). Security aceita: a divergência não emite ingresso e alerta.
2. **Hold durante o pagamento** — estender/congelar (arquiteto, QA) × aceitar o estorno residual (QA perguntou). → **Consenso em camadas**: o pedido **prende** os holds até `expira_em + 2 min` (não roubáveis durante o pagamento) e o estorno cobre o residual (webhook após `expirado`, conflito na conversão).
3. **`convertido` fora do índice** (achado do backend-dev, antecipado pelo QA) → **Consenso**: índice e `ON CONFLICT` com `status IN ('ativo','convertido')`; o `DO UPDATE` só rouba `ativo` vencido; ocupação conta `convertido`. `ingressos` com índice próprio como 2ª defesa. Complementa o ADR 0008.
4. **Dedup do webhook** — `webhook_eventos` × `stripe_events` × `processed_messages`. → **Consenso**: tabela própria `stripe_eventos(event_id PK)` na TX do pivô (ownership do `pedido`).
5. **Orquestração** — todos contra fila/Actor para a saga. → **Consenso**: pivô síncrono na api; estorno e reconciliação como jobs no worker (estado de banco, não DLQ).
6. **Breaker** — `sony/gobreaker` × código próprio. → **Consenso**: código próprio com relógio injetado (sem dependência nova, testável).
7. **Tempo** — SRE/arquiteto: E10. → **Consenso** (o roadmap já o coloca no E10): spans OTel instrumentados agora, correlação por `trace_id` no Loki.
8. **Replay da DLQ** — CLI × endpoint. → **Consenso** (arquiteto, security, SRE): só CLI `-mode=replay` (sem superfície HTTP nova, §6.6).
9. **Rate limit** — **Consenso**: criação de pedido 10/min por IP e 5/min por dono; webhook com limite folgado por IP só contra DoS.
10. **ADR da saga** → **Escalado**: usuário **autorizou o ADR 0010**.
11. **PaymentIntent + Element × Checkout Session** (arquiteto/backend × security) → **Escalado**: usuário escolheu **PaymentIntent + Payment Element** (CSP para o Stripe no E8).
12. **Token do ingresso** → **Escalado**: usuário escolheu **HMAC(segredo, ingresso_id)** — nada em claro no banco, reenvio idêntico, segredo versionado.
13. **Quantidade de tasks** → **Escalado**: usuário aceitou **6 tasks** (roadmap atualizado).

## Conclusão

- Escopo: checkout com **um pivô transacional e uma compensação** (estorno), recuperação por reconciliação; `notificacao` real fica no E7 (aqui só o evento + consumidor stub), telas no E8. **ADR 0010** criado.
- Riscos priorizados: (1) pagamento sem ingresso → estorno automático + alerta; (2) webhook forjado → assinatura + cruzamento; (3) corridas webhook × expiração × reconciliação → CAS + testes determinísticos; (4) T1b/T3 estourarem 30 arquivos → handler migra para a task seguinte.
- Dependência nova: `stripe-go` (Context7 + OSV + `lib.md` na task 0024, depguard só em `pagamento/`).

## Exigências por task

### T1 (0022) — Reserva: holds vendidos ocupam o assento + portas transacionais

- Migration (com down): índice único parcial `holds_assento_ativo` recriado como `WHERE status IN ('ativo','convertido')`; `ON CONFLICT` da trava com o **mesmo predicado**; `DO UPDATE ... WHERE holds.status='ativo' AND holds.expires_at <= agora`.
- Ocupação: `(status='ativo' AND expires_at > agora) OR status='convertido'`.
- Métodos transacionais recebendo `outbox.Tx` (só estes): `PrenderParaPedido(dono, sessao, códigos, até)` — valida que são vivos e do dono, fixa `expires_at` sem consumir `extensoes_usadas`; `Converter(dono, sessao, códigos)` — `ativo → convertido`, **idempotente**; `LiberarDoPedido` — volta a `liberado`.
- Sweeper nunca toca `convertido`. Depguard ajustado se preciso.
- Testes de regressão (PG real): upsert não rouba `convertido` nem hold preso; sweeper ignora `convertido`; ocupação mostra vendido; corrida canônica e suíte do E4 verdes; E2E M3 verde.

### T2 (0023) — Pedido: aggregate, máquina de estados e criação (gateway fake)

- Migrations: `pedidos` (UUID, `codigo` não sequencial ≥ 64 bits, `email`, `usuario_id` opcional, `dono_hash`, `sessao_id`, `total_centavos`, `status`, `expira_em`, `payment_intent_id`, `motivo_estorno`, `tentativas_estorno`, timestamps), `pedido_eventos` (append-only, só IDs), `ingressos` (UNIQUE `(sessao_id, assento_codigo) WHERE status='ativo'`, `versao_token`), índice parcial "1 pendente por dono".
- Aggregate `Pedido` + tabela de transições + `Transicionar` (`exhaustive`); matriz estado×evento **completa**, declarada no teste independente do código.
- Repositório com CAS (linhas afetadas); teste concorrente N goroutines → exatamente 1.
- `POST /pedidos`: carrinho (cookie) + `X-Requested-With`; e-mail obrigatório; 1–6 assentos; total de `sessao.preco` via porta síncrona (nunca do cliente); prende holds (TX) → commit → cria PaymentIntent pela porta `pagamento.Gateway` (fake nesta task) fora da TX; falha → libera holds, `falhou`, 503. Resposta: `pedido_id`, `codigo`, `total`, `expira_em`, `client_secret`.
- `GET /pedidos/{id}` só para o dono (404 caso contrário).
- Rate limit 10/min IP e 5/min dono; 409 com pedido pendente existente.
- Fake do gateway **programável** (falha na N-ésima, latência, registro de chamadas) — o mesmo do E12.
- `checkout_funil_total{etapa}` com labels fixos. Sem PII em log.

### T3 (0024) — Stripe + webhook + pivô

- `stripe-go` validada (Context7 + OSV) e registrada no `lib.md`; depguard: só `internal/pedido/pagamento`.
- Adapter Stripe: PaymentIntent com `metadata.pedido_id`, `Idempotency-Key`, timeout 5 s, retry 3× só rede/5xx com jitter; breaker próprio (5 falhas → aberto 30 s → `POST /pedidos` 503 + `Retry-After` + libera holds); métricas `gateway_requests_total{op,resultado}`, `gateway_duration_seconds`, `gateway_breaker_state`.
- `POST /webhooks/stripe`: corpo bruto ≤ 64 KB, `ConstructEvent` (300 s), 400 sem detalhe; fora do anti-CSRF; rate limit folgado por IP. TX única sem I/O externo: `stripe_eventos` → cruzamento → CAS → `Converter` → ingressos (token HMAC) → outbox `pedido-confirmado` (só `pedido_id`). Divergência/expirado/conflito → `estorno_pendente` com motivo. `payment_intent.payment_failed` → `falhou` + libera holds. Desconhecido → 2xx sem efeito. 2xx após commit; 5xx em falha.
- Segredos: `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `INGRESSO_TOKEN_SEGREDO` por env; `.env.example`; gitleaks `sk_`/`rk_`/`whsec_`; boot recusa `sk_live_` e `MORFEU_GATEWAY=fake` em produção. Nunca logar body nem `Stripe-Signature`.
- Compose: serviço Stripe CLI com `profiles: ["stripe"]`.
- Testes sem rede: assinatura gerada no teste; adulterado em 1 byte, JSON reformatado, timestamp ± fora da tolerância, sem header → 400 sem efeito; valor divergente; 10 entregas concorrentes do mesmo evento → 1 efeito; fixtures em `testdata/`.

### T4 (0025) — Estorno + reconciliação (worker)

- Job de estorno: `estorno_pendente` com `SKIP LOCKED`, lote limitado, `Idempotency-Key` `estorno-{pedido_id}`, backoff por `tentativas_estorno`; sucesso → `estornado` + libera holds/ingressos.
- Reconciliador (1–2 min, ≤ 20 pedidos/rodada, mesmo breaker): expira vencidos (libera holds + cancela PI); consulta o Stripe para `aguardando_pagamento` antigos e aplica **a mesma função do pivô**.
- Corridas determinísticas: webhook × expiração (duas ordens; expirado ⇒ estorno), roubo residual ⇒ estorno, reconciliação × webhook tardio ⇒ efeito único; crash entre passos não duplica.
- Compensações com o fake: cobrança falha → `falhou` + holds liberados; emissão em conflito → rollback + estorno 1× com a mesma key; estorno falhando → `estorno_pendente` observável.

### T5 (0026) — Observabilidade da saga + consumidor stub + integração ponta a ponta

- Métricas `saga_compensacoes_total{passo}`, `pedidos_presos{estado}`, `saga_pago_ate_emitido_seconds`, `reconciliacao_corrigidos_total`; spans OTel na saga/gateway (sem Tempo).
- Regras de alerta versionadas: estorno > 0 em 10 min, `estorno_pendente > 0`, presos > 10 min, p95 pago→emitido > 5 s, breaker aberto.
- Consumidor stub de `pedido-confirmado` (dedup `processed_messages`) — o envio real é do E7; e-mail falhando **nunca** compensa (teste).
- Teste de integração de API ponta a ponta (PG + Rabbit reais, gateway fake): trava → pedido → webhook assinado → ingressos → outbox → evento consumido.
- Limpeza: pedidos `expirado`/`falhou` com mais de 30 dias (mantém `pedido_eventos`) e outbox publicada com mais de 7 dias, em lotes.

### T6 (0027) — Replay da DLQ + hardening do worker

- `-mode=replay -fila <nome> [-limite N] [-dry-run]`: republica com confirm **antes** de remover da DLQ, preservando `message_id` (dedup garante idempotência); log só com IDs e contagem.
- `dlq_depth{fila}` + alerta (> 0 por 5 min).
- Shutdown gracioso (SIGTERM → para de consumir, espera em voo ≤ 25 s, `stop_grace_period: 30s`), prefetch 10, ack após commit, panic recovery por mensagem → DLQ, health do worker.
- Testes: replay 2× → efeito 1×; interrupção no meio sem perda/duplicação; mensagem que segue falhando volta à DLQ sem loop; SIGTERM com mensagem em voo.

### Exigências transferidas

- **E7**: consumidor real de `pedido-confirmado` busca e-mail por `pedido_id`; QR com token HMAC; e-mail nunca compensa.
- **E8**: Payment Element com CSP `script-src`/`frame-src`/`connect-src` para o Stripe; consulta de convidado por e-mail + código (rate limit, respostas indistinguíveis); Playwright do checkout com gateway fake.
- **E9**: cancelamento do operador — porta `convertido → liberado` na `reserva` + estorno pelo mesmo job.
- **E10**: Tempo + dashboards do funil e compensações.

### Perguntas escaladas ao usuário (respondidas em 2026-09-30)

1. ADR da saga → **autorizado (ADR 0010)**.
2. Integração Stripe → **PaymentIntent + Payment Element**.
3. Token do ingresso → **HMAC(segredo, ingresso_id)**.
4. Quebra do épico → **6 tasks** (roadmap atualizado).
