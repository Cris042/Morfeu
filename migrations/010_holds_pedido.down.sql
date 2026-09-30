-- Rollback da 010 (task 0022). Seguro enquanto não houver pedidos pagos: com
-- holds 'convertido', o assento pode ter também um 'ativo' e o índice antigo
-- não conflita — mas a trava voltaria a vender o mesmo assento duas vezes.
DROP INDEX IF EXISTS idx_holds_pedido;
DROP INDEX IF EXISTS holds_assento_ocupado;
CREATE UNIQUE INDEX holds_assento_ativo ON holds (sessao_id, assento_codigo) WHERE status = 'ativo';
ALTER TABLE holds DROP COLUMN IF EXISTS pedido_id;
