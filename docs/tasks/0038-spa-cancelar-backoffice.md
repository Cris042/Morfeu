# Task 0038 — SPA: cancelar pedido (cliente) + backoffice de filmes e salas (E9, T3a)

- **Data:** 2026-10-01
- **Status:** concluída (auditoria APROVADA em 2026-10-01)
- **Branch:** `feature/0038-spa-cancelar-backoffice` (da main `4403b0b`)
- **PRD:** docs/prd/0038-spa-cancelar-backoffice.md
- **Item do roadmap:** E9 — Backoffice restante (3ª de 4 — a T3 do refinamento foi dividida em 0038/0039 por tamanho, como previsto). Refinamento: `docs/refinamentos/E9-backoffice.md` §T1 (SPA do cancelamento) e §T3.

## Objetivo

O cliente cancela pelo SPA (conta e consulta de convidado) e o operador ganha a área `/backoffice` com filmes (TMDB, arquivar) e salas (layout em JSON).

## Escopo

Botão "Cancelar pedido" com confirmação; área do operador lazy com guarda de papel; filmes e salas; E2E do cancelamento do convidado.

## Fora de escopo

Sessões e pedidos do operador + E2E do operador (0039).

## Arquivos esperados

25 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

Ver PRD (CA01–CA07).

## Riscos

- Guarda de papel confundida com autorização → a API decide (RBAC testado na 0037).

## Estimativa de impacto

Baixo: SPA apenas; chunk novo do backoffice.
