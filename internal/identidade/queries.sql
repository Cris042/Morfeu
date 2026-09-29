-- name: InserirUsuario :execrows
-- Registro/seed (task 0009). ON CONFLICT em vez de erro de constraint: 0
-- linhas = e-mail em uso, sem o domínio depender do tipo de erro do driver.
INSERT INTO usuario (id, nome, email, senha_hash, papel)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (email) DO NOTHING;

-- name: BuscarUsuarioPorEmail :many
-- :many + LIMIT 1 em vez de :one: ausência = slice vazio (sem pgx.ErrNoRows
-- no domínio — RNF01 do PRD 0009).
SELECT id, nome, email, senha_hash, papel
FROM usuario
WHERE email = $1
LIMIT 1;

-- name: BuscarUsuarioPorID :many
SELECT id, nome, email, papel
FROM usuario
WHERE id = $1
LIMIT 1;
