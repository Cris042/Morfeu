# ADR 0011 — Cancelamento como nova entrada da saga

- **Status:** aceito
- **Data:** 2026-10-01
- **Task/PRD relacionados:** refinamento E9 (`docs/refinamentos/E9-backoffice.md`), tasks 0036–0038, `doc.md` §2 (cliente cancela até 2h antes) e §10 (auditabilidade); estende o ADR 0010

## Contexto

O ADR 0010 fechou a saga do checkout com um pivô (`aguardando_pagamento → pago`) e uma compensação (estorno), disparada só por falhas do próprio pagamento (`divergencia`, `tardio`, `emissao`), e deixou o cancelamento "reservado ao E9". O E9 traz três novas entradas que devolvem dinheiro de um pedido **já pago**: o cliente cancela até 2h antes da sessão; o operador cancela um pedido individual; o operador cancela uma sessão com ingressos vendidos. Os 5 agentes do refinamento convergiram em reaproveitar o estorno existente; a forma do cancelamento da sessão foi escolhida pelo usuário (porta transacional), que também autorizou este ADR.

## Escopo

Cobre: as transições novas da máquina de estados do pedido, o destino dos ingressos e dos assentos, a fronteira `sessao` ↔ `pedido` no cancelamento da sessão e o fechamento da corrida "pagamento confirmado depois do cancelamento da sessão". Não cobre: telas (tasks 0036/0038), trilha de auditoria (task 0037 — prevista no `doc.md`, sem ADR), check-in (fase 2).

## Decisão

**Cancelar é entrar na compensação existente: `pago → estorno_pendente` por CAS, com os ingressos cancelados na mesma TX e os assentos liberados só quando o estorno conclui.**

- **Transição nova:** `pago → estorno_pendente`, com `motivo_estorno` ∈ {`cancelamento` (cliente), `operador`, `sessao_cancelada`} somados aos do ADR 0010. Nenhum estado novo: o status `cancelado` reservado no ADR 0010 **não** é criado — o motivo já distingue a origem e o estado `estornado` é o fim de todos os caminhos. Só pedido `pago` é cancelável; os demais estados respondem com o estado atual (idempotente) ou recusam.
- **Ingressos cancelados na hora:** a mesma TX do CAS marca os ingressos `ativo → cancelado`, de modo que o link `/i/*` responde 410 imediatamente, mesmo que o estorno demore (backoff até 1 h). Ingresso `usado` bloqueia o cancelamento.
- **Assentos liberados só no `estornado`:** os holds `convertido` continuam ocupando o assento até o job `ExecutarEstornos` concluir; `Reserva.LiberarDoPedido`, já chamado na TX do `estornado`, passa a liberar também os holds `convertido` (até aqui só existiam estornos de pedidos que nunca converteram). Assim, um assento nunca volta à venda com dinheiro ainda não devolvido.
- **Regras do cliente no domínio:** `agora ≤ inicio_sessao − 2h` (fronteira inclusiva, relógio injetado, início da sessão pela porta já existente). A API devolve `cancelavel` calculado no servidor. O operador não tem janela, mas não cancela pedido de sessão já iniciada.
- **Cancelamento da sessão por porta transacional:** o `sessao` declara a porta `CancelarPedidosDaSessao(ctx, tx, sessaoID)`; o `pedido` a implementa; o composition root liga. Numa TX única, o `sessao` cancela a sessão e chama a porta, que move em lote todos os pedidos `pago` da sessão para `estorno_pendente` (`sessao_cancelada`) e cancela seus ingressos. Mesma forma da porta `pedido → reserva` do ADR 0010, agora no sentido `sessao → pedido`; nenhum módulo importa o outro.
- **Pivô checa a sessão:** dentro da TX do pivô, pagamento de pedido cuja sessão não está mais ativa vai para `estorno_pendente` (`sessao_cancelada`) em vez de emitir — fecha a corrida com pedidos `aguardando_pagamento` no momento do cancelamento.
- **Estorno inalterado:** o job existente, a chave `estorno-{pedido}` e o e-mail `pedido.estornado` (E7) atendem os novos motivos sem mudança de contrato.

## Tecnologias ou padrões envolvidos

Compare-and-swap SQL e máquina de estados por tabela (ADR 0005), ports & adapters com TX compartilhada (ADR 0003/0010), transactional outbox (ADR 0007).

## Benefícios

- Zero estado novo e zero job novo: a compensação já testada (dedup, backoff, alerta, e-mail) passa a servir também ao cancelamento.
- Atomicidade: sessão cancelada, pedidos em estorno e ingressos invalidados nascem juntos ou não nascem.
- Nenhum assento é revendido antes de o dinheiro voltar.

## Trade-offs

- Mais um sentido de acoplamento transacional entre módulos (`sessao → pedido`), além do `pedido → reserva`.
- Cancelar uma sessão grande é uma TX proporcional ao número de pedidos (UPDATE em lote; aceitável na volumetria de um cinema).
- O assento fica indisponível entre o cancelamento e o estorno concluído (minutos, ou até horas com o gateway instável).
- Sem estado `cancelado`, o histórico distingue cancelamento de falha só pelo motivo.

## Riscos

- **Corrida cancelamento × webhook × job** — prob. média, impacto alto. Mitigação: CAS em toda transição; testes determinísticos das ordens em PG real.
- **Estorno em massa contra o rate limit do gateway** — prob. baixa, impacto médio. Mitigação: lote limitado e backoff do job existente; alerta de idade do `estorno_pendente`.
- **Abuso por operador comprometido** — prob. baixa, impacto alto. Mitigação: trilha de auditoria na mesma TX (task 0037) e RBAC testado em todas as rotas.

## Estratégias para minimizar os trade-offs

- A porta nova é única e mínima; testada em PG real com 0, 1 e N pedidos em estados mistos.
- Métrica `cancelamentos_total{origem}` e o gauge de `estorno_pendente` existente tornam visível a fila de devolução.

## Condições de reversão

Revisitar se: (a) surgir check-in real (fase 2) — ingresso `usado` passa a ser comum e o cancelamento parcial por assento pode virar requisito; (b) cancelamentos de sessão com milhares de pedidos tornarem a TX única um gargalo medido — aí evento `sessao.cancelada` com consumidor idempotente (alternativa abaixo).

## Impacto esperado

Migration que amplia o CHECK de `motivo_estorno`; novos métodos no domínio/serviço/queries do `pedido`; porta nova no `sessao`; checagem no pivô; rotas de cancelamento do cliente (logado e convidado) e do operador.

## Alternativas consideradas e descartadas

- **Evento `sessao.cancelada` no outbox + consumidor no `pedido`** (proposta do arquiteto): evita acoplar TXs, mas traz consistência eventual, consumidor e testes de reentrega para uma operação rara; o usuário escolheu a porta transacional.
- **Estado `cancelado` no pedido:** duplicaria `estorno_pendente`/`estornado` para o mesmo fim; o motivo cumpre o papel.
- **Liberar os assentos já no cancelamento:** devolveria o assento à venda antes de o dinheiro voltar e quebraria a regra do ADR 0010 (liberação no `estornado`).
- **Ingressos cancelados só no estorno:** deixaria um ingresso válido nas mãos do cliente enquanto o estorno está pendente.
- **Janela de 2h calculada na SPA:** relógio do navegador não é confiável; o servidor decide e expõe `cancelavel`.
