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

-- name: InserirRefresh :exec
-- Abre família (login) ou insere o sucessor (rotação) — task 0010.
INSERT INTO refresh_token (id, usuario_id, familia_id, hash, expira_em)
VALUES ($1, $2, $3, $4, $5);

-- name: TravarRefreshPorHash :many
-- FOR UPDATE: dois refreshes concorrentes do mesmo token serializam aqui; o
-- segundo enxerga usado_em e vira reuso (RF05 do PRD 0010). :many p/ não
-- depender de pgx.ErrNoRows no domínio.
SELECT r.id, r.usuario_id, r.familia_id, r.expira_em, r.usado_em, r.revogado_em, u.papel
FROM refresh_token r
JOIN usuario u ON u.id = r.usuario_id
WHERE r.hash = $1
LIMIT 1
FOR UPDATE OF r;

-- name: MarcarRefreshUsado :exec
UPDATE refresh_token SET usado_em = now() WHERE id = $1;

-- name: RevogarFamilia :execrows
UPDATE refresh_token SET revogado_em = now()
WHERE familia_id = $1 AND revogado_em IS NULL;

-- name: RevogarFamiliaPorHash :execrows
-- Logout: revoga a família a que o token apresentado pertence.
UPDATE refresh_token SET revogado_em = now()
WHERE familia_id = (SELECT atual.familia_id FROM refresh_token AS atual WHERE atual.hash = $1)
  AND revogado_em IS NULL;

-- name: RevogarTodasDoUsuario :execrows
UPDATE refresh_token SET revogado_em = now()
WHERE usuario_id = $1 AND revogado_em IS NULL;

-- name: PseudonimizarUsuario :execrows
-- Deleção de conta (RF07): preserva id/papel; e-mail tombstone único e
-- minúsculo libera o original; '!' não é hash PHC válido — nenhum login casa.
UPDATE usuario
SET nome = 'Conta removida',
    email = 'removido+' || id::text || '@invalido.local',
    senha_hash = '!',
    atualizado_em = now()
WHERE id = $1 AND papel = 'cliente';

-- name: ApagarRefreshExpirados :execrows
DELETE FROM refresh_token WHERE expira_em < now();
