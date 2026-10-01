# Task 0036 — Backend: cancelamento pelo cliente e cancelamento de sessão com vendidos (E9, T1)

- **Data:** 2026-10-01
- **Status:** em andamento
- **Branch:** `feature/0036-cancelamento` (da main `7a1d87e`)
- **PRD:** docs/prd/0036-cancelamento.md
- **Item do roadmap:** E9 — Backoffice restante (1ª de 3). Refinamento: `docs/refinamentos/E9-backoffice.md` §T1. ADR 0011.

## Objetivo

Cliente (logado ou convidado) cancela o pedido pago até 2h antes da sessão e o operador cancela uma sessão com ingressos vendidos — ambos entram na compensação existente (`pago → estorno_pendente`), com os ingressos cancelados na hora e os assentos liberados no estorno.

## Escopo

Migration do motivo; máquina de estados; serviço/rotas do cancelamento (logado e convidado); `cancelavel` na visão do pedido; porta `sessao → pedido` com TX única no cancelamento da sessão; checagem de sessão cancelada no pivô; métrica `cancelamentos_total{origem}`; testes de integração.

## Fora de escopo

Botão "Cancelar" na SPA e E2E do cancelamento (movidos para a 0038 — o total passaria de 30 arquivos, conforme previsto no refinamento); auditoria e cancelamento pelo operador (0037).

## Arquivos esperados

~25 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

Ver PRD (CA01–CA10).

## Riscos

- Corrida pivô × cancelamento da sessão → trava de todas as linhas de pedido da sessão na TX do cancelamento + checagem da sessão depois do `FOR UPDATE` no pivô.

## Estimativa de impacto

Médio: migration de CHECK, rotas novas, mudança no pivô e no cancelamento da sessão (resposta 204 → 200 com contagem).
