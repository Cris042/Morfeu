-- Dataset sintético do teste de carga (PRD 0045 RF07). Determinístico e
-- idempotente: rodar 2× não duplica (chaves naturais + ON CONFLICT/NOT EXISTS).
-- SÓ roda em banco cujo nome termina em _carga.
--
-- Uso (depois do app subir: as migrations rodam no boot):
--   docker compose -p morfeu-carga --project-directory . -f deploy/carga/docker-compose.carga.yml \
--     exec -T postgres psql -U postgres -d morfeu_carga -v ON_ERROR_STOP=1 \
--     [-v salas=20 -v dias=7 -v slots=8 -v fileiras=10 -v colunas=20 -v filmes=40 -v usuarios=200] \
--     < deploy/carga/seed.sql
--
-- Dimensionamento padrão: 20 salas × 7 dias × 8 sessões = 1120 sessões de 200
-- assentos = 224 000 assentos (≥ 10× o que a disputa e o checkout consomem num
-- run); cada sala tem sessões de 2 h encostadas (fim = início + 100 min + 20 min
-- de limpeza), sem conflito na EXCLUDE de horário. A grade de dias é ancorada em
-- 00:00 UTC de amanhã: rodar de novo no mesmo dia não cria nada; em outro dia
-- só acrescenta os dias novos.
--
-- Usuários carga{n}@example.test: o hash Argon2id abaixo é de uma senha
-- ALEATÓRIA DESCARTADA (ninguém loga com eles — a carga não faz login). Para
-- logar, registre uma conta pela API ou gere outro hash (docs/carga/runbook.md).
\set ON_ERROR_STOP on
\if :{?salas} \else \set salas 20 \endif
\if :{?dias} \else \set dias 7 \endif
\if :{?slots} \else \set slots 8 \endif
\if :{?fileiras} \else \set fileiras 10 \endif
\if :{?colunas} \else \set colunas 20 \endif
\if :{?filmes} \else \set filmes 40 \endif
\if :{?usuarios} \else \set usuarios 200 \endif

-- Guarda: nunca semear um banco que não seja de carga.
DO $$
BEGIN
    IF current_database() !~ '_carga$' THEN
        RAISE EXCEPTION 'seed de carga recusado: o banco "%" não termina em _carga', current_database();
    END IF;
END
$$;

BEGIN;

-- Filmes: tmdb_id sintético (9 000 001…) é a chave de idempotência.
INSERT INTO filmes (titulo, sinopse, duracao_min, ano, tmdb_id)
SELECT 'Carga Filme ' || lpad(n::text, 3, '0'),
       'Filme sintético do teste de carga ' || n || '.',
       90 + (n % 60),
       1990 + (n % 35),
       9000000 + n
FROM generate_series(1, :filmes) AS n
ON CONFLICT (tmdb_id) DO NOTHING;

-- Salas: nome único = chave de idempotência; layout sem vãos nem PCD.
INSERT INTO salas (nome, layout)
SELECT 'Carga Sala ' || lpad(n::text, 2, '0'),
       jsonb_build_object('fileiras', :fileiras, 'colunas', :colunas, 'vaos', '[]'::jsonb, 'pcd', '[]'::jsonb)
FROM generate_series(1, :salas) AS n
ON CONFLICT (nome) DO NOTHING;

-- Sessões futuras: slot k começa às 08:00 UTC + 2 h × k, de amanhã em diante.
-- Filme em rodízio determinístico. NOT EXISTS por (sala, início) = idempotência.
WITH base AS (
    SELECT (date_trunc('day', now() AT TIME ZONE 'UTC') + interval '1 day') AS dia0
), pool AS (
    SELECT array_agg(id ORDER BY tmdb_id) AS ids
    FROM filmes
    WHERE tmdb_id BETWEEN 9000001 AND 9000000 + :filmes
), grade AS (
    SELECT sa.id AS sala_id,
           substring(sa.nome FROM '[0-9]+$')::int AS n,
           d, k,
           ((base.dia0 + (d - 1) * interval '1 day' + interval '8 hours' + k * interval '2 hours') AT TIME ZONE 'UTC') AS inicio
    FROM salas sa
    CROSS JOIN base
    CROSS JOIN generate_series(1, :dias) AS d
    CROSS JOIN generate_series(0, :slots - 1) AS k
    WHERE sa.nome ~ '^Carga Sala [0-9]+$'
      AND substring(sa.nome FROM '[0-9]+$')::int <= :salas
)
INSERT INTO sessoes (filme_id, sala_id, inicio, duracao_min, fim, preco_centavos)
SELECT pool.ids[1 + ((g.n + g.d + g.k) % cardinality(pool.ids))],
       g.sala_id, g.inicio, 100, g.inicio + interval '120 minutes', 3200
FROM grade g
CROSS JOIN pool
WHERE NOT EXISTS (SELECT 1 FROM sessoes x WHERE x.sala_id = g.sala_id AND x.inicio = g.inicio)
ORDER BY g.sala_id, g.inicio;

-- Usuários sintéticos (id determinístico a partir do e-mail).
INSERT INTO usuario (id, nome, email, senha_hash, papel)
SELECT md5('carga-usuario-' || n)::uuid,
       'Carga Usuario ' || n,
       'carga' || n || '@example.test',
       '$argon2id$v=19$m=19456,t=2,p=1$G1Y5V6RVkcXrpwFSzmobUg$yQt8IwvxKf8x0vTUrYdyWAL+OIa8NnGljcthy503EZM',
       'cliente'
FROM generate_series(1, :usuarios) AS n
ON CONFLICT (email) DO NOTHING;

COMMIT;

-- Estatísticas frescas: o planner do run enxerga o dataset, não tabelas vazias.
ANALYZE filmes;
ANALYZE salas;
ANALYZE sessoes;
ANALYZE usuario;
ANALYZE holds;
ANALYZE pedidos;
ANALYZE ingressos;
