-- Rollback da 016 (task 0036).
DROP INDEX IF EXISTS idx_pedidos_sessao;
ALTER TABLE pedidos DROP CONSTRAINT IF EXISTS pedidos_motivo_estorno_check;
ALTER TABLE pedidos ADD CONSTRAINT pedidos_motivo_estorno_check
    CHECK (motivo_estorno IN ('divergencia', 'tardio', 'emissao'));
