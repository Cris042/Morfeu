# Task 0030 — E-mail de estorno (E7, T3)

- **Data:** 2026-09-30
- **Status:** concluída (auditoria APROVADA em 2026-09-30)
- **Branch:** `feature/0030-email-estorno` (da main `da2dae1`)
- **PRD:** docs/prd/0030-email-estorno.md
- **Item do roadmap:** E7 — Notificação (3ª e última; fecha o E7). Refinamento: `docs/refinamentos/E7-notificacao.md` §T3.

## Objetivo

Avisar o cliente quando o estorno automático é concluído (doc.md: "aviso de estorno"): evento `pedido.estornado` na mesma TX do estorno, fila própria e um e-mail curto sem QR nem ingresso.

## Escopo

Evento na TX de `estornarUm`; porta `pedido.DadosParaAvisoDeEstorno`; fila `notificacao.pedido_estornado` + DLX/DLQ (lista do replay e gauge); consumidor tipado; `AvisoDeEstorno` + template; testes.

## Fora de escopo

Cancelamento pelo operador (E9, reutilizará o mesmo aviso); página pública do ingresso (E8).

## Arquivos esperados

17 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [x] Estorno concluído → exatamente 1 `pedido.estornado`; pago tarde nunca gera `pedido.confirmado`.
- [x] Aviso sem QR, sem link de ingresso; só pedido estornado é avisado.
- [x] Fila nova no replay e no gauge de DLQ.

## Riscos

- Nenhum novo: mesmo padrão da 0026/0028.

## Estimativa de impacto

Baixo.
