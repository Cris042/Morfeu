# Task 0034 — Backend: consulta de convidado e página do ingresso (E8, T4)

- **Data:** 2026-09-30
- **Status:** em andamento
- **Branch:** `feature/0034-consulta-ingresso` (da main `93b49fd`)
- **PRD:** docs/prd/0034-consulta-ingresso.md
- **Item do roadmap:** E8 — SPA checkout + convidado/conta (4ª de 5). Refinamento: `docs/refinamentos/E8-spa-checkout.md` §T4.

## Objetivo

Dar ao convidado acesso ao pedido (e-mail + código) e tornar válido o link do e-mail `/i/{id}.{token}`: dados do ingresso e QR, com expiração e revogação, sem oráculo de existência.

## Escopo

`POST /pedidos/consulta`, `GET /i/{ref}`, `GET /i/{ref}/qr.png`, rate limits, headers, redação do link no access log e no span, porta da sessão e gerador de QR injetados pelo main.

## Fora de escopo

Telas (0035); Caddy (E0c-CD — redação do path no access log do proxy registrada como pendência).

## Arquivos esperados

16 (lista no PRD).

## Dependências esperadas

Nenhuma nova (QR reaproveita o `skip2/go-qrcode` da notificação).

## Critérios de aceite

- [ ] Consulta: acerto com links; todo "não encontrado" idêntico byte a byte; teto por IP e por e-mail.
- [ ] Ingresso: HMAC sempre calculado + `hmac.Equal`; 404 idêntico; 410 só depois do token válido; versão do token respeitada.
- [ ] Headers `no-referrer`/`no-store`/`nosniff` em toda resposta de `/i/*`; link fora do log e do span.

## Riscos

- Oráculo por tempo de resposta (id existe × não existe) → mesmo cálculo de HMAC nos dois caminhos; diferença residual só da consulta indexada.

## Estimativa de impacto

Baixo/médio: rotas novas, nenhuma migration.
