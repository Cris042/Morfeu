-- Task: 0023 — Pedido: aggregate, máquina de estados e criação (E6 T2)
-- PRD: docs/prd/0023-pedido-criacao.md · ADR 0010
-- Descrição: tabelas do módulo pedido.
--   pedidos: estado da saga (CAS por status), total SEMPRE calculado no
--     servidor, código público não sequencial; no máximo 1 pedido
--     aguardando pagamento por carrinho (índice único parcial).
--   pedido_eventos: trilha append-only das transições (só IDs, sem PII —
--     doc.md §7 auditabilidade).
--   ingressos: segunda linha de defesa contra venda dupla (índice único
--     parcial por assento ativo); preenchida no pivô (task 0024).
--   FKs para sessoes são integridade declarada (como em holds); o pedido não
--   lê sessoes (preço vem pela porta do módulo sessao — ADR 0003).
-- Rollback: 011_pedidos.down.sql.

CREATE TABLE pedidos (
    id                 UUID PRIMARY KEY,
    codigo             VARCHAR(16)  NOT NULL UNIQUE CHECK (codigo ~ '^[A-Z2-7]{16}$'),
    email              VARCHAR(254) NOT NULL,
    usuario_id         UUID,
    dono_hash          BYTEA        NOT NULL CHECK (octet_length(dono_hash) = 32),
    sessao_id          BIGINT       NOT NULL REFERENCES sessoes (id),
    assentos           VARCHAR(3)[] NOT NULL CHECK (cardinality(assentos) BETWEEN 1 AND 6),
    total_centavos     BIGINT       NOT NULL CHECK (total_centavos > 0),
    status             VARCHAR(24)  NOT NULL CHECK (status IN
                           ('aguardando_pagamento', 'pago', 'expirado', 'falhou', 'estorno_pendente', 'estornado')),
    expira_em          TIMESTAMPTZ  NOT NULL,
    payment_intent_id  VARCHAR(255) UNIQUE,
    motivo_estorno     VARCHAR(16)  CHECK (motivo_estorno IN ('divergencia', 'tardio', 'emissao')),
    tentativas_estorno SMALLINT     NOT NULL DEFAULT 0,
    criado_em          TIMESTAMPTZ  NOT NULL,
    atualizado_em      TIMESTAMPTZ  NOT NULL
);

-- 1 pedido pendente por carrinho (refinamento E6, security).
CREATE UNIQUE INDEX pedidos_pendente_por_dono ON pedidos (dono_hash) WHERE status = 'aguardando_pagamento';

CREATE TABLE pedido_eventos (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pedido_id   UUID        NOT NULL REFERENCES pedidos (id),
    de          VARCHAR(24),
    para        VARCHAR(24) NOT NULL,
    ocorrido_em TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_pedido_eventos_pedido ON pedido_eventos (pedido_id);

CREATE TABLE ingressos (
    id             UUID PRIMARY KEY,
    pedido_id      UUID        NOT NULL REFERENCES pedidos (id),
    sessao_id      BIGINT      NOT NULL REFERENCES sessoes (id),
    assento_codigo VARCHAR(3)  NOT NULL CHECK (assento_codigo ~ '^[A-Z][1-9][0-9]?$'),
    status         VARCHAR(10) NOT NULL CHECK (status IN ('ativo', 'cancelado', 'usado')),
    versao_token   SMALLINT    NOT NULL DEFAULT 1,
    criado_em      TIMESTAMPTZ NOT NULL
);
-- Segunda linha de defesa contra venda dupla (a primeira é holds_assento_ocupado).
CREATE UNIQUE INDEX ingressos_assento_ativo ON ingressos (sessao_id, assento_codigo) WHERE status = 'ativo';
CREATE INDEX idx_ingressos_pedido ON ingressos (pedido_id);
