-- Task: 0009 — Identidade: usuário, registro, login, seed do operador (E1)
-- PRD: docs/prd/0009-identidade-registro-login.md
-- Descrição: cria usuario (ownership: módulo identidade). E-mail único e já
--   normalizado pela aplicação (CHECK garante minúsculas); papel restrito a
--   cliente|operador (doc.md §5). A PK/UNIQUE cobrem os dois padrões de acesso
--   (por id no /auth/eu, por e-mail no login) — nenhum índice extra.
--   Tabela nova e vazia: sem impacto em dados existentes.
-- Rollback: 005_usuario.down.sql (DROP TABLE).

CREATE TABLE IF NOT EXISTS usuario (
    id            UUID         PRIMARY KEY,
    nome          VARCHAR(120) NOT NULL,
    email         TEXT         NOT NULL,
    senha_hash    TEXT         NOT NULL,
    papel         TEXT         NOT NULL,
    criado_em     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    atualizado_em TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT usuario_email_key UNIQUE (email),
    CONSTRAINT usuario_email_minusculo CHECK (email = lower(email)),
    CONSTRAINT usuario_papel_valido CHECK (papel IN ('cliente', 'operador'))
);
