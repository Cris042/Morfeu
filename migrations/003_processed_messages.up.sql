-- Task: 0005 — Consumidor idempotente + DLQ (E0b, parte 2/2)
-- PRD: docs/prd/0005-consumidor-idempotente.md
-- Descrição: cria processed_messages (ownership excepcional da plataforma,
--   RN03 do PRD 0005 — como outbox_events): registro de dedup do consumidor
--   idempotente, gravado NA MESMA TX do efeito de domínio (RF05, ADR 0007).
--   PK composta (message_id, consumidor) é o próprio índice do único padrão
--   de acesso (INSERT ... ON CONFLICT DO NOTHING) — nenhum índice extra.
--   Tabela nova e vazia: sem impacto em dados existentes nem lock relevante.
-- Rollback: 003_processed_messages.down.sql (DROP TABLE).

CREATE TABLE IF NOT EXISTS processed_messages (
    message_id   UUID        NOT NULL,
    consumidor   TEXT        NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, consumidor)
);
