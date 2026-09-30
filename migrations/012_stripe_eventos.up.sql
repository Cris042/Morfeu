-- Task: 0024 — Stripe + webhook + pivô (E6 T3)
-- PRD: docs/prd/0024-stripe-webhook-pivo.md · ADR 0010
-- Descrição: dedup dos eventos do webhook do Stripe por event_id, gravado na
--   MESMA TX do efeito (pivô) — entrega duplicada ou concorrente do mesmo
--   evento produz um único efeito (doc.md §14.1). Tabela própria do módulo
--   pedido (processed_messages é do consumidor do broker — refinamento E6).
-- Rollback: 012_stripe_eventos.down.sql.

CREATE TABLE stripe_eventos (
    event_id    VARCHAR(255) PRIMARY KEY,
    tipo        VARCHAR(64)  NOT NULL,
    recebido_em TIMESTAMPTZ  NOT NULL
);
