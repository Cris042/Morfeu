-- name: ListarFilmesPublicos :many
-- Cartaz público (RF02 do PRD 0011): só não arquivados.
SELECT id, titulo, sinopse, duracao_min, ano, poster_url, imdb_id, tmdb_id
FROM filmes
WHERE arquivado_em IS NULL
ORDER BY criado_em DESC, id DESC
LIMIT 100;

-- name: BuscarFilmePublico :many
-- :many + LIMIT 1: ausência = vazio (domínio sem pgx.ErrNoRows).
SELECT id, titulo, sinopse, duracao_min, ano, poster_url, imdb_id, tmdb_id
FROM filmes
WHERE id = $1 AND arquivado_em IS NULL
LIMIT 1;

-- name: ListarFilmesBackoffice :many
-- Backoffice (RF03): inclui arquivados.
SELECT id, titulo, sinopse, duracao_min, ano, poster_url, imdb_id, tmdb_id, arquivado_em
FROM filmes
ORDER BY criado_em DESC, id DESC
LIMIT 500;

-- name: InserirFilme :one
-- id por IDENTITY (migration 007). Usada pelo backoffice e pela CLI na mesma
-- TX do evento catalogo.filme_criado (outbox).
INSERT INTO filmes (titulo, sinopse, duracao_min, ano, poster_url, imdb_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, titulo, sinopse, duracao_min, ano, poster_url, imdb_id, tmdb_id;

-- name: AtualizarFilme :many
-- Substitui os campos editáveis (RF03). tmdb_id não é editável à mão.
UPDATE filmes
SET titulo = $2, sinopse = $3, duracao_min = $4, ano = $5,
    poster_url = $6, imdb_id = $7, atualizado_em = now()
WHERE id = $1
RETURNING id, titulo, sinopse, duracao_min, ano, poster_url, imdb_id, tmdb_id;

-- name: ArquivarFilme :execrows
-- Idempotente: reaplicar mantém a data original (COALESCE); 0 linhas = inexistente.
UPDATE filmes
SET arquivado_em = COALESCE(arquivado_em, now()), atualizado_em = now()
WHERE id = $1;

-- name: UpsertFilmeProjetado :exec
-- Projeção de catalogo.filme_criado (RF07, task 0005). Roda dentro da TX do
-- wrapper de dedup (outbox.ProcessarUmaVez): com dedup correto o conflito
-- nunca ocorre; se ocorrer, aplicacoes > 1 denuncia a falha (prova de RN01).
INSERT INTO catalogo_filmes_projetados (film_id, titulo, ano)
VALUES ($1, $2, $3)
ON CONFLICT (film_id) DO UPDATE
SET titulo       = EXCLUDED.titulo,
    ano          = EXCLUDED.ano,
    aplicacoes   = catalogo_filmes_projetados.aplicacoes + 1,
    projetado_em = now();

-- name: BuscarFilmeProjetado :one
SELECT film_id, titulo, ano, aplicacoes, projetado_em
FROM catalogo_filmes_projetados
WHERE film_id = $1;
