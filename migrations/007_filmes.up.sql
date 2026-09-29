-- Task: 0011 — Catálogo: migração films→filmes + CRUD do operador (E2 T1)
-- PRD: docs/prd/0011-catalogo-filmes-crud.md
-- Descrição: alinha o catálogo herdado da E0a ao doc.md §5 ANTES da FK do E3:
--   tabela/colunas em PT (ADR 0004), id por IDENTITY (fim do MAX(id)+1 —
--   corrida sob concorrência), tmdb_id único (import idempotente, task 0012),
--   arquivamento em vez de delete (RN01), CHECKs de sanidade. Dados
--   PRESERVADOS: só renames/ALTERs; a identity recomeça acima do maior id.
--   Tabela pequena (seeds): locks de ALTER são instantâneos; sem produção.
-- Rollback: 007_filmes.down.sql (reverte integralmente para films/EN).

ALTER TABLE films RENAME TO filmes;
ALTER TABLE filmes RENAME COLUMN title TO titulo;
ALTER TABLE filmes RENAME COLUMN year TO ano;
ALTER TABLE filmes RENAME COLUMN runtime TO duracao_min;
ALTER TABLE filmes RENAME COLUMN synopsis TO sinopse;
ALTER TABLE filmes RENAME COLUMN created_at TO criado_em;
ALTER TABLE filmes ALTER COLUMN criado_em TYPE TIMESTAMPTZ USING criado_em AT TIME ZONE 'UTC';
ALTER TABLE filmes ALTER COLUMN criado_em SET DEFAULT now();

ALTER TABLE filmes ADD COLUMN tmdb_id BIGINT;
ALTER TABLE filmes ADD COLUMN arquivado_em TIMESTAMPTZ;
ALTER TABLE filmes ADD COLUMN atualizado_em TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE filmes ADD CONSTRAINT filmes_tmdb_id_key UNIQUE (tmdb_id);
ALTER TABLE filmes ADD CONSTRAINT filmes_titulo_nao_vazio CHECK (char_length(btrim(titulo)) >= 1);
ALTER TABLE filmes ADD CONSTRAINT filmes_duracao_valida CHECK (duracao_min IS NULL OR duracao_min BETWEEN 1 AND 1440);

ALTER TABLE filmes ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY;
SELECT setval(pg_get_serial_sequence('filmes', 'id'), COALESCE((SELECT MAX(id) FROM filmes), 0) + 1, false);
