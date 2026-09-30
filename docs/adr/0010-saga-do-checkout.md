# ADR 0010 — Saga do checkout: pivô síncrono no webhook, estorno como única compensação

- **Status:** aceito
- **Data:** 2026-09-30
- **Task/PRD relacionados:** refinamento E6 (`docs/refinamentos/E6-saga-checkout.md`), tasks 0022–0027, `doc.md` fluxo crítico 6.1 e §14

## Contexto

O E6 materializa a compra (`doc.md` §6.1): hold → cobrança no Stripe → emissão do ingresso → e-mail. É a candidata nº 5 do `doc.md` §11 e o contrato entre três módulos (`pedido`, `reserva`, `notificacao`) e um sistema externo (Stripe). O risco nº 2 do roadmap é a "saga superdimensionada no imaginário": com e-mail pós-pivô, a saga real tem **um** pivô e **uma** compensação verdadeira. Os 5 agentes do refinamento convergiram para uma forma síncrona com reconciliação; o usuário autorizou este ADR e escolheu PaymentIntent + Payment Element e token do ingresso por HMAC.

## Escopo

Cobre: estados do pedido e o pivô; ciclo de vida do hold durante o pagamento; onde vivem os ingressos e como o hold vira ingresso sem violar o ADR 0003; forma da orquestração (síncrona × assíncrona); compensação; reconciliação; integração Stripe; token do ingresso. Não cobre: envio do e-mail e template (E7), telas do checkout (E8), cancelamento/estorno a pedido do operador (E9), Tempo (E10).

## Decisão

**O checkout é um caso de uso síncrono com um pivô transacional no webhook do Stripe; o estorno é a única compensação; tudo o que falha fora da TX é recuperado por jobs de reconciliação no worker — sem fila para a saga.**

- **Estados** (enum + tabela de transições + `Transicionar`, ADR 0005): `aguardando_pagamento → pago` (**pivô**); falhas: `expirado`, `falhou`; pós-falha tardia: `estorno_pendente → estornado`; `cancelado` reservado ao E9. Sem `confirmado` (o e-mail fica fora da máquina), sem `em_pagamento`, sem `divergente`/`estornando` — o motivo do estorno (`divergencia`, `tardio`, `emissao`) e as tentativas são colunas. Toda transição persistida é **compare-and-swap** (`UPDATE ... WHERE id=$1 AND status=$esperado`, checando linhas afetadas).
- **Criação do pedido** (`POST /pedidos`, api): valida que os holds são vivos e do dono do carrinho e cobrem o conjunto (1–6); total = `sessao.preco × n` em centavos, nunca do cliente; no máximo 1 pedido `aguardando_pagamento` por carrinho; `expira_em = agora + 15 min`; **prende os holds** até `expira_em + 2 min` (método da `reserva` que não consome a extensão do cliente); commit; **depois** cria o PaymentIntent (fora da TX) com `metadata.pedido_id` e `Idempotency-Key`. Falha do gateway → libera os holds, pedido `falhou`, 503.
- **Hold vendido continua ocupando o assento:** o índice único parcial da `reserva` passa a ser `WHERE status IN ('ativo','convertido')`; o upsert só rouba hold `ativo` vencido; a ocupação conta `convertido`. O `UNIQUE (sessao_id, assento_codigo) WHERE status='ativo'` em `ingressos` é a segunda linha de defesa.
- **Pivô** (`POST /webhooks/stripe`, api, rota pública): verifica a assinatura sobre o **corpo bruto** (tolerância 300 s, ≤ 64 KB); numa TX curta, sem I/O externo: dedup `stripe_eventos(event_id PK)` → cruza `amount`, `currency` e `metadata.pedido_id` com o pedido → CAS `aguardando_pagamento → pago` → `reserva.Converter` (holds → `convertido`) → insere ingressos → grava outbox `pedido-confirmado` (só `pedido_id`); 2xx só após o commit, 5xx em falha (o Stripe reenvia).
- **Fronteira `pedido` ↔ `reserva`:** porta síncrona declarada no consumidor (`pedido`), com os métodos recebendo a `outbox.Tx` aberta pelo service do `pedido` (único dono da TX). Nenhum módulo importa o outro; o adapter é ligado no composition root. Mesma forma do `FonteSessoes` (E4), agora transacional.
- **Compensação:** pagamento que chega com o pedido `expirado`, com valor divergente ou com conflito na conversão/emissão → `estorno_pendente` (motivo registrado). O **estorno** é executado por um job no worker, com `Idempotency-Key` `estorno-{pedido_id}`, retry com backoff; acima do limite de tentativas o pedido continua `estorno_pendente` e dispara alerta (é estado de banco, não DLQ). E-mail é pós-pivô: falha → retry → DLQ → alerta; **nunca** estorna.
- **Reconciliação** (worker, periódica, `SKIP LOCKED`, lote limitado): expira pedidos vencidos (libera holds e cancela o PaymentIntent) e consulta o Stripe para pedidos presos, aplicando **a mesma função do webhook** (protegida pelo CAS). Cobre webhook perdido com a api fora do ar.
- **Stripe:** PaymentIntent + Payment Element (o cartão nunca toca o backend; CSP para o Stripe no E8). Porta `pagamento.Gateway` com dois adapters: `stripe-go` e fake programável (CI sem rede + modo load-test do E12); fake recusado em produção. Timeout 5 s, retry só em rede/5xx, circuit breaker **só no gateway** (código próprio com relógio injetado).
- **Token do ingresso:** `HMAC-SHA256(segredo, ingresso_id)` em base64url — nada em claro no banco, reenvio idêntico possível; o segredo vem do ambiente e tem versão (coluna `versao_token`) para permitir rotação.

## Tecnologias ou padrões envolvidos

Transactional outbox (ADR 0007), compare-and-swap SQL, máquina de estados por tabela (ADR 0005), ports & adapters (ADR 0003), idempotency key do Stripe, webhook assinado (HMAC-SHA256), reconciliação periódica, circuit breaker, `stripe-go` (versão registrada no `lib.md` na task do webhook).

## Benefícios

- Um único pivô transacional: pagamento confirmado, holds convertidos, ingressos e evento nascem juntos ou não nascem.
- Sem fila nem orquestrador para a saga: menos estados intermediários, menos código, alinhado ao risco nº 2 do roadmap.
- Correção independente de entrega do webhook: CAS + dedup + reconciliação tornam webhook duplicado, atrasado ou perdido inofensivos.
- Venda dupla impossível em duas camadas (índice da `reserva` com `convertido` + índice em `ingressos`).
- Testável sem rede: assinatura gerada no teste, gateway fake programável, relógio injetado.

## Trade-offs

- O handler do webhook faz trabalho de domínio síncrono (latência de resposta ao Stripe = uma TX); aceitável na volumetria.
- A `reserva` passa a expor métodos que recebem uma transação alheia — acoplamento transacional entre módulos (explícito, mas real).
- Reconciliação por polling consome chamadas ao Stripe (limite de ~25 req/s em test mode) e introduz latência de até um ciclo na recuperação.
- HMAC derivado: vazamento do segredo permite gerar todos os tokens válidos.
- O assento fica preso até 17 min por pedido abandonado.

## Riscos

- **Pagamento confirmado sem ingresso** (conflito ou pedido expirado) — prob. baixa, impacto alto (cliente cobrado). Mitigação: estorno automático idempotente + alerta de `estorno_pendente`.
- **Webhook forjado** — prob. média numa rota pública, impacto crítico. Mitigação: assinatura sobre corpo bruto + cruzamento de valor/pedido.
- **Estorno falhando repetidamente** — prob. baixa, impacto alto. Mitigação: estado observável, alerta, retry idempotente.
- **Corrida webhook × expiração × reconciliação** — prob. média, impacto alto. Mitigação: CAS em toda transição + testes determinísticos das ordens.
- **Vazamento do segredo HMAC** — prob. baixa, impacto alto. Mitigação: segredo só em env, versão por ingresso, rotação.

## Estratégias para minimizar os trade-offs

- A porta transacional é mínima (prender, converter, liberar) e testada em PG real; nenhum outro método da `reserva` recebe `Tx`.
- Reconciliação com lote limitado e o mesmo breaker do gateway.
- Métricas `saga_compensacoes_total{passo}`, `pedidos_presos{estado}`, `saga_pago_ate_emitido_seconds` e alertas versionados detectam degradação.
- Limpeza de pedidos abandonados e da outbox publicada evita crescimento sem fim.

## Condições de reversão

Revisitar se: (a) a emissão passar a envolver chamada externa (ex.: bilhetagem de terceiros) — aí a saga precisa de passos assíncronos e um orquestrador persistido; (b) houver mais de um gateway ou meios assíncronos (PIX/boleto, fase 2) — os estados de pagamento pendente crescem; (c) a latência do webhook ou o volume tornarem a TX síncrona um gargalo medido.

## Impacto esperado

Novo módulo `internal/pedido` (aggregate, estados, service, handler, `checkout/`, `pagamento/`), novas tabelas `pedidos`, `pedido_eventos`, `ingressos`, `stripe_eventos`; migration na `reserva` (índice com `convertido` + portas transacionais); jobs de reconciliação e estorno no worker; `stripe-go` no `lib.md`; perfil `stripe` no compose (Stripe CLI, só dev).

## Alternativas consideradas e descartadas

- **Orquestrador assíncrono por eventos** (cada passo publica e o próximo consome): dobraria estados e mensagens para um fluxo com um único pivô; o Stripe já reentrega o webhook. Overengineering (roles.md §1).
- **Status `em_pagamento` no hold/pedido:** o índice parcial + CAS já garantem a segurança; estado extra sem ganho.
- **Checkout Session hospedado:** tira o cartão da origem e dispensa CSP (preferência do security), mas a sessão expira em no mínimo 30 min — conflita com o hold de 15 min — e redireciona o cliente. Escolha do usuário: PaymentIntent + Element.
- **Ingresso emitido pela `reserva`:** a `reserva` deixaria de ser dona só de holds e o aggregate `Pedido 1→N Ingresso` ficaria fragmentado.
- **Dedup do webhook em `processed_messages`:** é o registro do consumidor do broker; misturar origens confunde a chave e o ownership.
- **Token aleatório em claro no banco / hash com rotação no reenvio:** o primeiro expõe ingressos num vazamento do banco; o segundo invalida o QR anterior a cada reenvio. Escolha do usuário: HMAC derivado.
- **`sony/gobreaker`:** half-open sofisticado desnecessário; ~40 linhas próprias com relógio injetado evitam dependência nova.

## ADRs relacionados

- ADR 0003 (fronteiras) — a porta transacional `pedido → reserva` é a forma aprovada aqui de cruzar módulos numa TX.
- ADR 0005 (DDD tático) — aggregate `Pedido`, máquina de estados por tabela, Strategy em `pagamento.Gateway`.
- ADR 0007 (RabbitMQ) — outbox do `pedido-confirmado`; replay de DLQ entra no E6 por CLI.
- ADR 0008 (trava) — **complementado**: `convertido` passa a ocupar o índice único parcial; o roubo continua só sobre `ativo` vencido.
- ADR 0006 (testes) — corridas determinísticas e gateway fake.
