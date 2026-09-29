-- Trava de assento (ADR 0008, PRD 0015): um hold vivo por (sessão, assento)
-- garantido pelo índice único parcial. "Vivo" = status 'ativo' e expires_at
-- no futuro (expiração lazy); o sweeper só marca vencidos como 'expirado'.
CREATE TABLE holds (
    id               UUID PRIMARY KEY,
    sessao_id        BIGINT      NOT NULL REFERENCES sessoes (id),
    assento_codigo   VARCHAR(3)  NOT NULL CHECK (assento_codigo ~ '^[A-Z][1-9][0-9]?$'),
    dono_hash        BYTEA       NOT NULL CHECK (octet_length(dono_hash) = 32),
    status           VARCHAR(10) NOT NULL CHECK (status IN ('ativo', 'liberado', 'expirado', 'convertido')),
    expires_at       TIMESTAMPTZ NOT NULL,
    extensoes_usadas SMALLINT    NOT NULL DEFAULT 0 CHECK (extensoes_usadas BETWEEN 0 AND 1),
    criado_em        TIMESTAMPTZ NOT NULL,
    atualizado_em    TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX holds_assento_ativo ON holds (sessao_id, assento_codigo) WHERE status = 'ativo';
CREATE INDEX idx_holds_expira_ativo ON holds (expires_at) WHERE status = 'ativo';
CREATE INDEX idx_holds_dono_ativo ON holds (dono_hash) WHERE status = 'ativo';
