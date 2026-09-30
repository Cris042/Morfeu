# PRD 0023 — Pedido: aggregate, máquina de estados e criação com gateway fake (E6, T2)

- **Task:** docs/tasks/0023-pedido-criacao.md
- **Branch:** feature/0023-pedido-criacao
- **Data:** 2026-09-30
- **Status:** em andamento

## Objetivo

Criar o módulo `pedido` do checkout (ADR 0010). O cliente com assentos travados abre um pedido e recebe o segredo da cobrança. O total é calculado no servidor, o prazo é de 15 min e os holds ficam presos na mesma TX. A cobrança sai pela porta `pagamento.Gateway`, que nesta task é o fake programável; o Stripe entra na 0024. Fontes: `docs/refinamentos/E6-saga-checkout.md` §T2, ADR 0010, ADR 0005 (aggregate + State por tabela), ADR 0003 (portas).

## Escopo

Migration 011, aggregate `Pedido`, máquina de estados e CAS, `POST /pedidos`, `GET /pedidos/{id}`, porta de preço (`sessao`), porta transacional (`reserva`, adapter no `main`), gateway fake, rate limit, métrica do funil, depguard e testes.

## Fora de escopo

- Stripe, webhook, `stripe_eventos`, emissão de ingressos e token HMAC (0024).
- Estorno, reconciliação e cancelamento da cobrança de pedido vencido (0025).
- Métricas da saga e alertas (0026).
- Vínculo do pedido com a conta (`usuario_id`, coluna criada e ainda não preenchida) e retomada de um pedido pendente pelo SPA (E8).
- Recusar o gateway fake em produção (0024, junto com a config do gateway).

## Requisitos funcionais

- RF01 — `POST /pedidos` (cookie do carrinho + `X-Requested-With: morfeu`, corpo ≤ 4 KB, campos desconhecidos recusados): `{email, sessao_id, assentos[1..6]}` → `201 {pedido: {id, codigo, sessao_id, assentos, total_centavos, status, expira_em}, client_secret}`.
- RF02 — **Total = preço da sessão × assentos, em centavos, sempre calculado no servidor** (doc.md §14.2). Qualquer campo de valor enviado pelo cliente → 400.
- RF03 — Preço por assento pela porta `pedido.FonteSessoes` (`sessao.PrecoDaSessaoAberta`). Sessão inexistente, cancelada ou iniciada → 404. O mapa público (`GET /sessoes/{id}/mapa`) passa a expor `preco_centavos`.
- RF04 — Em uma TX, o serviço insere o pedido (`aguardando_pagamento`, `expira_em = agora + 15 min`, código base32 de 80 bits) e registra a trilha. Em seguida prende os holds do carrinho até `expira_em + 2 min` pela porta transacional (`reserva.PrenderParaPedido` via adapter). Holds ausentes, vencidos ou de outro carrinho → 409 `holds_invalidos`, e nada fica gravado.
- RF05 — Depois do commit, a cobrança é criada no gateway **fora da TX** (`Idempotency` `pedido-{id}-cobranca`, moeda `brl`), e o id da cobrança é gravado no pedido. Se o gateway falhar: pedido `falhou` (CAS), holds devolvidos, 503 `pagamento_indisponivel` com `Retry-After`.
- RF06 — `GET /pedidos/{id}`: só o carrinho que criou o pedido (cookie). Outro carrinho, sem carrinho ou id inválido → 404. Nunca devolve o e-mail nem o hash do carrinho.
- RF07 — Rate limit na criação (limitador do E1): 10/min por IP e 5/min por carrinho → 429.
- RF08 — **1 pedido `aguardando_pagamento` por carrinho** (índice único parcial). Pendente no prazo → 409 `pedido_pendente` com o `pedido_id`. Corrida entre duas criações → uma vence e a outra recebe 409.
- RF09 — **Expiração lazy**: na criação, o pendente vencido do carrinho vira `expirado` (CAS) e devolve os assentos, numa TX própria confirmada antes da abertura. Assim a falha da abertura não o prende de novo. Os holds eram do pedido vencido: o cliente trava de novo e o SPA do E8 trata o 409 `holds_invalidos` voltando ao mapa.
- RF10 — Máquina de estados (ADR 0010) em tabela, com `Transicionar` que recusa transição ilegal. Toda transição persistida é CAS (`WHERE id AND status = de`); 0 linhas → `ErrTransicaoConcorrente`. A trilha `pedido_eventos` (de, para, instante; só IDs) é gravada na mesma TX.
- RF11 — Métrica `checkout_funil_total{etapa}` com etapas fixas: `pedido_criado`, `cobranca_criada`, `gateway_falhou`.

## Requisitos não funcionais

- RNF01 — O `pedido` não importa outros módulos. Sessão e reserva chegam por interfaces declaradas no consumidor; o adapter `reservaDoPedido` vive no `main` e traduz os erros da reserva (ADR 0003/0010). Depguard `pedido-domain` e `pedido-pagamento` (strict).
- RNF02 — Sem PII em logs: só `pedido_id`, `sessao_id` e contagens. O e-mail fica só na tabela.
- RNF03 — Relógio injetado. Testes com PG e Redis reais, sessão e reserva reais, gateway fake (sem rede).

## Regras de negócio

- RN01 — Máx. 6 assentos por pedido; e-mail sempre obrigatório (fluxo único convidado+conta).
- RN02 — O hold preso vence 2 min depois do pedido. Pagamento que chega depois disso vira estorno (0024/0025).
- RN03 — A regra `ate` da 0022 é mantida: o pedido fixa o prazo do hold em `expira_em + 2 min`, mesmo que o cliente o tenha estendido antes. O pedido governa (pendência da auditoria 0022, decidida aqui).

## Critérios de aceite

- [ ] CA01 — Caminho feliz: 201, total 2 × 3000 = 6000, prazo +15 min, `client_secret`, código de 16 caracteres; holds presos até +17 min; cobrança no fake com valor, moeda, pedido e chave corretos; `payment_intent_id` gravado.
- [ ] CA02 — Trilha `→ aguardando_pagamento`; funil `pedido_criado` e `cobranca_criada`.
- [ ] CA03 — Recusas sem pedido gravado e sem chamada ao gateway: total do cliente (400), e-mail inválido (400 com o campo), sem CSRF (403), sem carrinho ou carrinho forjado, assento de outro ou sem hold (409 `holds_invalidos`), sessão inexistente (404).
- [ ] CA04 — Validação em memória: e-mail (vazio, sem @, com nome, com espaço, > 254), assentos (0, 7, repetido, mal formado), 6 aceitos.
- [ ] CA05 — Pendente → 409 com o id. Depois do prazo, o pendente vira `expirado` com trilha, o assento volta, e o carrinho trava e cria um pedido novo.
- [ ] CA06 — `GET` do dono sem e-mail; outro carrinho, sem carrinho e id inválido → 404.
- [ ] CA07 — Gateway fora → 503 + `Retry-After`, `falhou` com trilha, ocupação sem os assentos, funil `gateway_falhou`, carrinho compra de novo.
- [ ] CA08 — Duas criações simultâneas do mesmo carrinho (5 rodadas) → exatamente 1 × 201 e 1 × 409.
- [ ] CA09 — Matriz estado × evento completa, declarada no teste de forma independente; pivô irreversível.
- [ ] CA10 — CAS com 20 caminhos concorrentes → exatamente 1 vence; trilha com 1 transição.
- [ ] CA11 — Rate limit por carrinho → 429.
- [ ] CA12 — Hold preso conta no teto de 6 (pendência da 0022).
- [ ] CA13 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Matriz de estados, `NovoPedido`, validação | unit | CA04, CA09 |
| Fake do gateway (falha na N-ésima, latência com contexto, registro) | unit | apoio a CA07 |
| Rotas reais com PG + Redis, sessão/reserva reais, relógio injetado | integração | CA01–CA03, CA05–CA08, CA10–CA12 |

## Plano de implementação

1. Migration 011 + queries + `sqlc generate`; preço no mapa da `sessao`.
2. `pagamento` (porta + fake).
3. Aggregate, estados, repositório com CAS, serviço, handler.
4. Wiring no `main` (adapter, limitadores, funil) + depguard.
5. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `golang-database`, `golang-testing`.

## Arquivos que serão criados

- `migrations/011_pedidos.up.sql`, `migrations/011_pedidos.down.sql`
- `internal/pedido/{pedido.go, estados.go, errors.go, repositorio.go, service.go, handler.go, queries.sql, dominio_test.go, handler_test.go}`
- `internal/pedido/db/{db.go, models.go, queries.sql.go}` (gerados)
- `internal/pedido/pagamento/{gateway.go, fake.go, fake_test.go}`
- `docs/tasks/0023-pedido-criacao.md`, `docs/prd/0023-pedido-criacao.md`

## Arquivos que serão modificados

- `internal/sessao/queries.sql`, `internal/sessao/db/queries.sql.go` (gerado), `internal/sessao/service.go`
- `cmd/morfeu/main.go`, `sqlc.yaml`, `.golangci.yml`
- `docs/tasks/README.md`, `plan.md`, `state.md`, `docs/roadmap.md`

Total: 30.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- Novas rotas `POST /pedidos` e `GET /pedidos/{id}`. O mapa ganha `preco_centavos`, campo aditivo que não quebra o SPA.
- Novas tabelas; `ingressos` já nasce com o índice da segunda linha de defesa, sem uso até a 0024.

## Riscos

- Pedido pendente bloqueia o carrinho por até 15 min se o cliente abandonar a tela. Mitigação: o 409 devolve o `pedido_id` para o E8 retomar, e a expiração lazy libera o carrinho no prazo.

## Estratégia de rollback

Reverter o merge e aplicar `011_pedidos.down.sql`. Nada fora do módulo depende das tabelas.
