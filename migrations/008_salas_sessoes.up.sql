-- Task: 0013 — Sessões e salas: escrita, layout e regra de não-conflito (E3 T1)
-- PRD: docs/prd/0013-sessoes-salas-escrita.md
-- Descrição: módulo sessao (ownership: salas, sessoes).
--   btree_gist permite a EXCLUDE com igualdade de sala_id + sobreposição de
--   intervalo: o BANCO garante que duas sessões 'agendada' nunca se sobrepõem
--   na mesma sala (doc.md §6.3; refinamento E3). fim = inicio + duração +
--   limpeza (20 min) é calculado na aplicação e persistido (índice GiST e 409
--   com o horário sem recalcular). Intervalo '[)': encostar no fim é permitido.
--   Sessão cancelada sai da EXCLUDE (WHERE status = 'agendada').
--   A FK sessoes.filme_id → filmes é integridade declarada; o módulo sessao
--   NÃO lê filmes (a duração vem por porta síncrona, snapshot na criação).
--   Índices: sala_id (FK); (filme_id, inicio) parcial p/ a leitura pública do
--   E5 ("sessões futuras de um filme"). Tabelas novas: sem impacto em dados.
--   CREATE EXTENSION exige privilégio de owner/superuser — ok no PG self-hosted;
--   em banco gerenciado, conferir a allowlist de extensões.
-- Rollback: 008_salas_sessoes.down.sql (DROP das tabelas; extensão mantida).

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE IF NOT EXISTS salas (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nome          VARCHAR(80) NOT NULL,
    layout        JSONB       NOT NULL,
    criado_em     TIMESTAMPTZ NOT NULL DEFAULT now(),
    atualizado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT salas_nome_key UNIQUE (nome),
    CONSTRAINT salas_nome_nao_vazio CHECK (char_length(btrim(nome)) >= 1)
);

CREATE TABLE IF NOT EXISTS sessoes (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    filme_id       BIGINT      NOT NULL REFERENCES filmes (id),
    sala_id        BIGINT      NOT NULL REFERENCES salas (id),
    inicio         TIMESTAMPTZ NOT NULL,
    duracao_min    INTEGER     NOT NULL,
    fim            TIMESTAMPTZ NOT NULL,
    preco_centavos INTEGER     NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'agendada',
    criado_em      TIMESTAMPTZ NOT NULL DEFAULT now(),
    atualizado_em  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sessoes_duracao_valida CHECK (duracao_min BETWEEN 1 AND 1440),
    CONSTRAINT sessoes_preco_valido CHECK (preco_centavos BETWEEN 100 AND 100000),
    CONSTRAINT sessoes_status_valido CHECK (status IN ('agendada', 'cancelada')),
    CONSTRAINT sessoes_fim_apos_inicio CHECK (fim > inicio),
    CONSTRAINT sessoes_sem_conflito EXCLUDE USING gist (
        sala_id WITH =,
        tstzrange(inicio, fim, '[)') WITH &&
    ) WHERE (status = 'agendada')
);

CREATE INDEX IF NOT EXISTS idx_sessoes_sala ON sessoes (sala_id);
CREATE INDEX IF NOT EXISTS idx_sessoes_filme_inicio ON sessoes (filme_id, inicio) WHERE status = 'agendada';
