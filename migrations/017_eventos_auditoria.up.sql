-- Task: 0037 — Trilha de auditoria + consulta e cancelamento de pedido pelo operador (E9 T2)
-- PRD: docs/prd/0037-auditoria-operador.md
-- Descrição: trilha enxuta das ações do operador (doc.md §10): só IDs —
--   ator, ação (enum fechado), alvo (tipo + id numérico ou UUID) e instante.
--   Os CHECKs impedem texto livre (nenhuma PII cabe em alvo_id). Append-only:
--   UPDATE e TRUNCATE barrados por trigger; DELETE fica livre só para a purga
--   de 12 meses (um único usuário de banco — limite documentado no PRD).
-- Rollback: 017_eventos_auditoria.down.sql (descarta a trilha).

CREATE TABLE IF NOT EXISTS eventos_auditoria (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ator_id     UUID        NOT NULL,
    acao        VARCHAR(24) NOT NULL CHECK (acao IN (
                    'filme_criado', 'filme_atualizado', 'filme_arquivado', 'filme_importado',
                    'sala_criada', 'sala_atualizada', 'sessao_criada', 'sessao_cancelada',
                    'pedido_cancelado')),
    alvo_tipo   VARCHAR(8)  NOT NULL CHECK (alvo_tipo IN ('filme', 'sala', 'sessao', 'pedido')),
    alvo_id     VARCHAR(36) NOT NULL CHECK (alvo_id ~ '^([0-9]{1,19}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$'),
    ocorrido_em TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_eventos_auditoria_ocorrido ON eventos_auditoria (ocorrido_em);
CREATE INDEX IF NOT EXISTS idx_eventos_auditoria_alvo ON eventos_auditoria (alvo_tipo, alvo_id, ocorrido_em);

CREATE OR REPLACE FUNCTION eventos_auditoria_imutavel() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'eventos_auditoria é append-only (%)', TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS eventos_auditoria_sem_update ON eventos_auditoria;
CREATE TRIGGER eventos_auditoria_sem_update BEFORE UPDATE ON eventos_auditoria
    FOR EACH ROW EXECUTE FUNCTION eventos_auditoria_imutavel();
DROP TRIGGER IF EXISTS eventos_auditoria_sem_truncate ON eventos_auditoria;
CREATE TRIGGER eventos_auditoria_sem_truncate BEFORE TRUNCATE ON eventos_auditoria
    FOR EACH STATEMENT EXECUTE FUNCTION eventos_auditoria_imutavel();
