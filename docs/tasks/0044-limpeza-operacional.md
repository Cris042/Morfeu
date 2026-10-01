# Task 0044 — Limpeza operacional (E11, T3)

- **Data:** 2026-10-01
- **Status:** em implementação
- **Branch:** `feature/0044-limpeza-operacional`
- **PRD:** docs/prd/0044-limpeza-operacional.md
- **Item do roadmap:** E11 — Hardening + backup (3ª e última). Refinamento: `docs/refinamentos/E11-hardening-backup.md` §T3 (decisão do usuário: sem apagar pedidos).

## Objetivo

Impedir o crescimento sem fim de `processed_messages` e de holds terminais sem abrir brecha no dedup do replay da DLQ.

## Escopo

Limpeza em lotes no worker (`processed_messages` > 30 dias; holds `liberado`/`expirado` > 7 dias); replay da DLQ retém mensagens mais velhas que a janela do dedup; índices (migration 019); métricas e alerta de limpeza parada.

## Fora de escopo

Pedidos (decisão do usuário); outbox publicada (já limpa desde o PRD 0027); trilha de auditoria (purga da 0037).

## Arquivos esperados

~22 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

Ver PRD (CA01–CA07).

## Riscos

- Replay tardio reprocessando efeito → replay retém o que passou da janela.

## Estimativa de impacto

Baixo: jobs novos no worker, uma guarda no replay e índices.
