-- Task: 0027 — Replay da DLQ + hardening do worker (E6 T6)
-- PRD: docs/prd/0027-replay-dlq-hardening.md · ADR 0010 · auditoria 0025 (NB2)
-- Descrição: cobranca_encerrada marca que a cobrança de um pedido que não
--   virou venda foi cancelada no gateway (ou já estava). Pedido expirado com
--   cobrança ainda aberta é varrido pela reconciliação: se o cliente pagou
--   depois (cancelamento recusado + webhook perdido), vira estorno.
-- Rollback: 014_pedidos_cobranca_encerrada.down.sql.

ALTER TABLE pedidos ADD COLUMN cobranca_encerrada BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX idx_pedidos_expirados_cobranca_aberta ON pedidos (atualizado_em)
    WHERE status = 'expirado' AND NOT cobranca_encerrada AND payment_intent_id IS NOT NULL;
