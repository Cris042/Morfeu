-- Task: 0044 — Limpeza operacional (E11 T3)
-- PRD: docs/prd/0044-limpeza-operacional.md
-- Descrição: índices de suporte à limpeza diária em lotes. processed_messages
--   (PK sem processed_at) e holds terminais (liberado/expirado) são apagados por
--   idade; sem índice cada lote varreria a tabela inteira. O índice de holds é
--   parcial: só linhas terminais, sem custo para o caminho quente (ativo/convertido).
-- Impacto: tabelas pequenas; criação rápida. Sem CONCURRENTLY (golang-migrate roda em TX).
-- Rollback: 019_limpeza_indices.down.sql (remove os índices).

CREATE INDEX IF NOT EXISTS processed_messages_processed_at_idx
    ON processed_messages (processed_at);

CREATE INDEX IF NOT EXISTS holds_terminais_atualizado_em_idx
    ON holds (atualizado_em)
    WHERE status IN ('liberado', 'expirado');
