# Task 0023 — Pedido: aggregate, máquina de estados e criação com gateway fake (E6, T2)

- **Data:** 2026-09-30
- **Status:** concluída (auditoria APROVADA em 2026-09-30)
- **Branch:** `feature/0023-pedido-criacao` (da main `ad2e1b0`)
- **PRD:** docs/prd/0023-pedido-criacao.md
- **Item do roadmap:** E6 — Saga do checkout (2ª de 6). Refinamento: `docs/refinamentos/E6-saga-checkout.md` §T2; ADR 0010.

## Objetivo

Nascer o módulo `pedido`: o cliente com assentos travados abre um pedido (total calculado no servidor, prazo de 15 min, holds presos na mesma TX) e recebe o segredo da cobrança criada pela porta `pagamento.Gateway` — por enquanto o fake programável.

## Escopo

Migration 011 (`pedidos`, `pedido_eventos`, `ingressos`); aggregate + máquina de estados por tabela + CAS; `POST /pedidos` e `GET /pedidos/{id}`; porta de preço no `sessao`; porta transacional para a `reserva` (adapter no `main`); gateway fake; rate limit; funil; depguard.

## Fora de escopo

Stripe, webhook e pivô (0024); estorno e reconciliação (0025); vínculo com conta/usuário e retomada do pedido pelo SPA (E8).

## Arquivos esperados

~29 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [x] Pedido criado com total do servidor, holds presos até o prazo + 2 min e cobrança no gateway.
- [x] Recusas: total do cliente, validação, sem carrinho, holds alheios, sessão indisponível — sem pedido órfão.
- [x] 1 pedido pendente por carrinho (com corrida); vencido expira de forma lazy.
- [x] Gateway fora → 503, pedido `falhou`, assentos devolvidos.
- [x] Matriz de estados completa e CAS concorrente → exatamente 1.

## Riscos

- Teto de 30 arquivos (módulo novo inteiro) → sessão e reserva mudam o mínimo.

## Estimativa de impacto

Alto (módulo novo), sem mudança no comportamento das rotas existentes além do `preco_centavos` no mapa.
