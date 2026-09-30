-- Task: 0025 — Estorno + reconciliação (E6 T4)
-- PRD: docs/prd/0025-estorno-reconciliacao.md · ADR 0010
-- Descrição: índices parciais das duas varreduras periódicas do worker —
--   pendentes vencidos (reconciliação) e estornos pendentes (job de
--   estorno). Parciais: só cobrem as linhas em trânsito, pequenos para sempre.
-- Rollback: 013_pedidos_tarefas.down.sql.

CREATE INDEX idx_pedidos_pendentes_prazo ON pedidos (expira_em) WHERE status = 'aguardando_pagamento';
CREATE INDEX idx_pedidos_estorno_pendente ON pedidos (atualizado_em) WHERE status = 'estorno_pendente';
