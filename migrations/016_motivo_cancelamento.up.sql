-- Task: 0036 — Backend: cancelamento pelo cliente e de sessão com vendidos (E9 T1)
-- PRD: docs/prd/0036-cancelamento.md
-- Descrição: ADR 0011 — cancelar é entrar no estorno: o motivo ganha
--   cancelamento (cliente), operador (0037) e sessao_cancelada. O cancelamento
--   da sessão trava todos os pedidos dela → índice na FK sessao_id (até aqui
--   sem índice; a tabela é pequena — CREATE INDEX simples basta).
-- Rollback: 016_motivo_cancelamento.down.sql (falha de propósito se algum
--   pedido já usar os motivos novos).

ALTER TABLE pedidos DROP CONSTRAINT IF EXISTS pedidos_motivo_estorno_check;
ALTER TABLE pedidos ADD CONSTRAINT pedidos_motivo_estorno_check
    CHECK (motivo_estorno IN ('divergencia', 'tardio', 'emissao', 'cancelamento', 'operador', 'sessao_cancelada'));

CREATE INDEX IF NOT EXISTS idx_pedidos_sessao ON pedidos (sessao_id);
