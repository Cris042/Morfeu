# Task 0039 — SPA: sessões e pedidos do operador + E2E do backoffice (E9, T3b)

- **Data:** 2026-10-01
- **Status:** concluída (auditoria APROVADA em 2026-10-01, após 1 correção)
- **Branch:** `feature/0039-spa-sessoes-pedidos` (sobre a 0038)
- **PRD:** docs/prd/0039-spa-sessoes-pedidos.md
- **Item do roadmap:** E9 — Backoffice restante (4ª e última). Refinamento: `docs/refinamentos/E9-backoffice.md` §T3.

## Objetivo

Fechar o backoffice: o operador programa e cancela sessões (vendo os pedidos que serão estornados) e consulta/cancela pedidos; E2E do operador no CI.

## Escopo

Abas Sessões e Pedidos; detalhe do pedido com histórico e cancelamento; conversão do horário do cinema para UTC; E2E do operador + semeadura do operador no workflow de E2E.

## Fora de escopo

Trilha de auditoria pela UI (sem requisito).

## Arquivos esperados

16 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

Ver PRD (CA01–CA06).

## Riscos

- Fuso do cinema: UTC−3 fixo (sem horário de verão desde 2019) — documentado em `paraUTCDoCinema`.

## Estimativa de impacto

Baixo: SPA + workflow de E2E.
