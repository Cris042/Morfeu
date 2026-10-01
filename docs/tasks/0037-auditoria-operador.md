# Task 0037 — Trilha de auditoria + consulta e cancelamento de pedido pelo operador (E9, T2)

- **Data:** 2026-10-01
- **Status:** concluída (auditoria APROVADA em 2026-10-01)
- **Branch:** `feature/0037-auditoria-operador` (da main `8ecc2a8`)
- **PRD:** docs/prd/0037-auditoria-operador.md
- **Item do roadmap:** E9 — Backoffice restante (2ª de 3). Refinamento: `docs/refinamentos/E9-backoffice.md` §T2. ADR 0011.

## Objetivo

Toda ação do operador deixa trilha (só IDs, na mesma TX, 12 meses) e o operador consulta pedidos e cancela um pedido individual.

## Escopo

Migration 017 + pacote de plataforma `auditoria` (registro, purga, job no worker, métricas, alerta); trilha nas mutações de catálogo, salas, sessões e no cancelamento pelo operador; `GET /backoffice/pedidos`, `GET /backoffice/pedidos/:id`, `POST /backoffice/pedidos/:id/cancelar`; teste de RBAC sobre todas as rotas `/backoffice/*`.

## Fora de escopo

Telas (0038); consulta da trilha pela API (sem requisito).

## Arquivos esperados

30 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

Ver PRD (CA01–CA09).

## Riscos

- Mutação sem trilha → trilha na mesma TX (erro = rollback).
- PII na trilha → CHECKs da tabela (enum + alvo numérico/UUID).

## Estimativa de impacto

Médio: toda mutação do backoffice passa a abrir TX.
