-- Task: 0031 — Backend do checkout e da conta (E8 T1)
-- PRD: docs/prd/0031-backend-checkout-conta.md
-- Descrição: "Meus pedidos" lista por usuario_id (preenchido só a partir do
--   JWT, desde esta task); índice parcial — pedidos de convidado não entram.
-- Rollback: 015_pedidos_usuario.down.sql.

CREATE INDEX idx_pedidos_usuario ON pedidos (usuario_id, criado_em DESC) WHERE usuario_id IS NOT NULL;
