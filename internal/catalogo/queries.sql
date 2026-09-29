-- name: ListFilms :many
SELECT
    id,
    title,
    year,
    runtime,
    synopsis,
    imdb_id,
    poster_url,
    created_at
FROM films
ORDER BY created_at DESC
LIMIT 100;

-- name: GetFilm :one
SELECT
    id,
    title,
    year,
    runtime,
    synopsis,
    imdb_id,
    poster_url,
    created_at
FROM films
WHERE id = $1;

-- name: InsertFilm :one
-- Usada por CreateFilm (RF02/task 0002): o subcomando CLI criar-filme insere o
-- filme e enfileira catalogo.filme_criado na mesma TX via outbox.Enqueue.
-- films.id é BIGINT sem identity/sequence (schema da migration 001); o próximo
-- id é calculado por MAX(id)+1 dentro do próprio statement — simplificação
-- aceitável para uma ferramenta de operador único (CLI, sem concorrência real);
-- colisão eventual é reportada como erro de constraint (exit code != 0 na CLI).
INSERT INTO films (id, title, year, runtime, synopsis)
SELECT COALESCE(MAX(id), 0) + 1, $1, $2, $3, $4
FROM films
RETURNING id, title, year, runtime, synopsis, imdb_id, poster_url, created_at;

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
