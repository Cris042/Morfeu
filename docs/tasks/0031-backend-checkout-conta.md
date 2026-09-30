# Task 0031 — Backend do checkout e da conta (E8, T1)

- **Data:** 2026-09-30
- **Status:** em andamento
- **Branch:** `feature/0031-backend-checkout-conta` (da main `6e9f880`)
- **PRD:** docs/prd/0031-backend-checkout-conta.md
- **Item do roadmap:** E8 — SPA checkout + convidado/conta (1ª de 5). Refinamento: `docs/refinamentos/E8-spa-checkout.md` §T1.

## Objetivo

Preparar a API para o checkout do SPA: vínculo do pedido com a conta (Bearer opcional), "Meus pedidos", retomada do pagamento de um pedido pendente e a rota de pagamento de teste (só com gateway fake) usada pelo E2E do M4.

## Escopo

`autenticacao.Opcional`; `usuario_id` no `POST /pedidos`; leitura do pedido por carrinho ou conta; `GET /pedidos`; `Gateway.RecuperarSegredo`; `POST /pedidos/{id}/retomar`; `POST /__teste/pagar/{id}`; migration 015 (índice).

## Fora de escopo

Telas (0032, 0033, 0035); consulta de convidado e ingresso (0034).

## Arquivos esperados

~26 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [ ] `usuario_id` só do JWT; convidado nulo; Bearer inválido → 401 sem pedido.
- [ ] "Meus pedidos" só da conta; pedido alheio → 404.
- [ ] Retomada só do carrinho dono, aguardando no prazo, sem cache.
- [ ] Rota de teste paga pelo webhook real e não existe sem o registro do gateway fake.

## Riscos

- Rota de teste em produção → impossível por construção (registrada só com fake + boot recusa fake em produção) + teste.

## Estimativa de impacto

Médio: rotas novas; nenhuma mudança de contrato das existentes além do Bearer opcional.
