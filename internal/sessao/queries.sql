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
-- Filtros opcionais (PRD 0014 RF05): sala e intervalo [desde, ate).
SELECT id, filme_id, sala_id, inicio, duracao_min, fim, preco_centavos, status
FROM sessoes
WHERE (sqlc.narg(sala_id)::bigint IS NULL OR sala_id = sqlc.narg(sala_id)::bigint)
  AND (sqlc.narg(desde)::timestamptz IS NULL OR inicio >= sqlc.narg(desde)::timestamptz)
  AND (sqlc.narg(ate)::timestamptz IS NULL OR inicio < sqlc.narg(ate)::timestamptz)
ORDER BY inicio DESC
LIMIT 200;

-- name: ListarSessoesFuturasDoFilme :many
-- Leitura pública (PRD 0014 RF01): índice parcial (filme_id, inicio) da 008.
-- Sem nenhum campo de filme (fronteira ADR 0003) — só sessão e sala.
SELECT s.id, s.sala_id, sa.nome AS sala_nome, s.inicio, s.fim, s.preco_centavos
FROM sessoes s
JOIN salas sa ON sa.id = s.sala_id
WHERE s.filme_id = $1 AND s.status = 'agendada' AND s.inicio > $2
ORDER BY s.inicio
LIMIT 100;

-- name: BuscarMapaDaSessao :many
-- Mapa público (PRD 0014 RF04): só sessão agendada que ainda não começou.
SELECT s.id, s.sala_id, sa.nome AS sala_nome, sa.layout
FROM sessoes s
JOIN salas sa ON sa.id = s.sala_id
WHERE s.id = $1 AND s.status = 'agendada' AND s.inicio > $2
LIMIT 1;

-- name: CancelarSessao :many
-- Idempotente; vazio = inexistente. A cancelada sai da EXCLUDE. Devolve o
-- filme_id p/ invalidar o cache público do filme (PRD 0014 RF03).
UPDATE sessoes SET status = 'cancelada', atualizado_em = now() WHERE id = $1
RETURNING filme_id;
