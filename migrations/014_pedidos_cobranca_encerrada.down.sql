-- Rollback da 014 (task 0027).
DROP INDEX IF EXISTS idx_pedidos_expirados_cobranca_aberta;
ALTER TABLE pedidos DROP COLUMN IF EXISTS cobranca_encerrada;
