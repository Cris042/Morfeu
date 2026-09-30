-- Task: 0022 — Reserva: holds vendidos ocupam o assento + portas do pedido (E6 T1)
-- PRD: docs/prd/0022-reserva-portas-pedido.md · ADR 0010 (complementa o ADR 0008)
-- Descrição: (1) hold 'convertido' (vendido) passa a ocupar o índice único
--   parcial — antes ele saía do índice e um novo hold podia nascer sobre
--   assento vendido; (2) pedido_id marca o hold preso a um pedido em
--   pagamento (sem FK: pedidos é de outro módulo — ADR 0003).
--   Tabela pequena; o índice é recriado na mesma TX da migration.
-- Rollback: 010_holds_pedido.down.sql.

ALTER TABLE holds ADD COLUMN pedido_id UUID;

DROP INDEX holds_assento_ativo;
CREATE UNIQUE INDEX holds_assento_ocupado ON holds (sessao_id, assento_codigo)
    WHERE status IN ('ativo', 'convertido');

-- Portas do pedido: prender/converter/liberar por pedido_id.
CREATE INDEX idx_holds_pedido ON holds (pedido_id) WHERE pedido_id IS NOT NULL;
