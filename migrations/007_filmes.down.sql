-- Rollback da 007 (task 0011): volta ao schema da E0a (films/EN), dados preservados.
ALTER TABLE filmes ALTER COLUMN id DROP IDENTITY IF EXISTS;
ALTER TABLE filmes DROP CONSTRAINT IF EXISTS filmes_duracao_valida;
ALTER TABLE filmes DROP CONSTRAINT IF EXISTS filmes_titulo_nao_vazio;
ALTER TABLE filmes DROP CONSTRAINT IF EXISTS filmes_tmdb_id_key;
ALTER TABLE filmes DROP COLUMN IF EXISTS atualizado_em;
ALTER TABLE filmes DROP COLUMN IF EXISTS arquivado_em;
ALTER TABLE filmes DROP COLUMN IF EXISTS tmdb_id;
ALTER TABLE filmes ALTER COLUMN criado_em SET DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE filmes ALTER COLUMN criado_em TYPE TIMESTAMP USING criado_em AT TIME ZONE 'UTC';
ALTER TABLE filmes RENAME COLUMN criado_em TO created_at;
ALTER TABLE filmes RENAME COLUMN sinopse TO synopsis;
ALTER TABLE filmes RENAME COLUMN duracao_min TO runtime;
ALTER TABLE filmes RENAME COLUMN ano TO year;
ALTER TABLE filmes RENAME COLUMN titulo TO title;
ALTER TABLE filmes RENAME TO films;
