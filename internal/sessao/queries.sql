-- name: InserirSala :one
INSERT INTO salas (nome, layout) VALUES ($1, $2)
RETURNING id, nome, layout;

-- name: BuscarSala :many
-- :many + LIMIT 1: ausência = vazio (domínio sem pgx.ErrNoRows).
SELECT id, nome, layout FROM salas WHERE id = $1 LIMIT 1;

-- name: ListarSalas :many
SELECT id, nome, layout FROM salas ORDER BY nome LIMIT 200;

-- name: AtualizarSala :many
UPDATE salas SET nome = $2, layout = $3, atualizado_em = now()
WHERE id = $1
RETURNING id, nome, layout;

-- name: ExisteSessaoFuturaNaSala :one
-- Imutabilidade do layout (RN03 do PRD 0013): sessão agendada ainda por vir.
SELECT EXISTS (
    SELECT 1 FROM sessoes WHERE sala_id = $1 AND status = 'agendada' AND fim > $2
) AS existe;

-- name: InserirSessao :one
-- A EXCLUDE sessoes_sem_conflito rejeita sobreposição (23P01 → 409).
INSERT INTO sessoes (filme_id, sala_id, inicio, duracao_min, fim, preco_centavos)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, filme_id, sala_id, inicio, duracao_min, fim, preco_centavos, status;

-- name: SessaoConflitante :many
-- Reconsulta após o 23P01 para devolver o horário conflitante no 409.
SELECT id, inicio, fim FROM sessoes
WHERE sala_id = $1 AND status = 'agendada'
  AND tstzrange(inicio, fim, '[)') && tstzrange(sqlc.arg(inicio)::timestamptz, sqlc.arg(fim)::timestamptz, '[)')
ORDER BY inicio
LIMIT 1;

-- name: ListarSessoesBackoffice :many
SELECT id, filme_id, sala_id, inicio, duracao_min, fim, preco_centavos, status
FROM sessoes
ORDER BY inicio DESC
LIMIT 200;

-- name: CancelarSessao :execrows
-- Idempotente; 0 linhas = inexistente. A cancelada sai da EXCLUDE.
UPDATE sessoes SET status = 'cancelada', atualizado_em = now() WHERE id = $1;
