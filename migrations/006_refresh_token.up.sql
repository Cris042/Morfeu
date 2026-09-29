-- Task: 0010 — Refresh rotativo + detecção de reuso + pseudonimização (E1 T2)
-- PRD: docs/prd/0010-refresh-pseudonimizacao.md
-- Descrição: cria refresh_token (ownership: identidade). Só o SHA-256 do
--   token é persistido (UNIQUE = lookup do refresh). familia_id agrupa a
--   cadeia de rotação: reuso de um token já usado/revogado revoga a família.
--   Índices: usuario_id (FK + revogação na deleção de conta), familia_id
--   (revogação por família), expira_em (limpeza periódica no worker).
--   Tabela nova e vazia: sem impacto em dados existentes.
-- Rollback: 006_refresh_token.down.sql (DROP TABLE, cascata dos índices).

CREATE TABLE IF NOT EXISTS refresh_token (
    id          UUID        PRIMARY KEY,
    usuario_id  UUID        NOT NULL REFERENCES usuario (id),
    familia_id  UUID        NOT NULL,
    hash        BYTEA       NOT NULL,
    expira_em   TIMESTAMPTZ NOT NULL,
    usado_em    TIMESTAMPTZ,
    revogado_em TIMESTAMPTZ,
    criado_em   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT refresh_token_hash_key UNIQUE (hash)
);

CREATE INDEX IF NOT EXISTS idx_refresh_token_usuario ON refresh_token (usuario_id);
CREATE INDEX IF NOT EXISTS idx_refresh_token_familia ON refresh_token (familia_id);
CREATE INDEX IF NOT EXISTS idx_refresh_token_expira ON refresh_token (expira_em);
