-- name: TravarDono :exec
-- Serializa as TXs de trava do MESMO dono (teto de holds — ADR 0008); solta
-- sozinho no commit/rollback.
SELECT pg_advisory_xact_lock(@namespace::int, @chave::int);

-- name: TentarTravaSweeper :one
-- Uma passada do sweeper por vez entre processos (worker e all).
SELECT pg_try_advisory_xact_lock(@namespace::int, @chave::int) AS obtido;

-- name: HoldsVivosDoDono :many
SELECT id, sessao_id, assento_codigo, expires_at, extensoes_usadas
FROM holds
WHERE dono_hash = @dono_hash AND status = 'ativo' AND expires_at > @agora
ORDER BY expires_at, sessao_id, assento_codigo;

-- name: TravarAssento :many
-- Único caminho de criação (ADR 0008): cria o hold ou rouba um vencido.
-- Sem linha no RETURNING = hold vivo de outro dono → 409.
INSERT INTO holds (id, sessao_id, assento_codigo, dono_hash, status, expires_at, extensoes_usadas, criado_em, atualizado_em)
VALUES (@id, @sessao_id, @assento_codigo, @dono_hash, 'ativo', @expires_at, 0, @agora, @agora)
ON CONFLICT (sessao_id, assento_codigo) WHERE status = 'ativo'
DO UPDATE SET id = EXCLUDED.id, dono_hash = EXCLUDED.dono_hash, expires_at = EXCLUDED.expires_at,
    extensoes_usadas = 0, criado_em = EXCLUDED.criado_em, atualizado_em = EXCLUDED.atualizado_em
WHERE holds.expires_at <= @agora
RETURNING id, sessao_id, assento_codigo, expires_at, extensoes_usadas;

-- name: BuscarHoldVivoDoDono :many
SELECT id, sessao_id, assento_codigo, expires_at, extensoes_usadas
FROM holds
WHERE id = @id AND dono_hash = @dono_hash AND status = 'ativo' AND expires_at > @agora;

-- name: EstenderHold :many
-- Guarda da extensão única (última linha de defesa; o aggregate é a primeira).
UPDATE holds
SET expires_at = @novo_expires_at, extensoes_usadas = extensoes_usadas + 1, atualizado_em = @agora
WHERE id = @id AND dono_hash = @dono_hash AND status = 'ativo' AND expires_at > @agora AND extensoes_usadas = 0
RETURNING id, sessao_id, assento_codigo, expires_at, extensoes_usadas;

-- name: LiberarHold :execrows
UPDATE holds
SET status = 'liberado', atualizado_em = @agora
WHERE id = @id AND dono_hash = @dono_hash AND status = 'ativo' AND expires_at > @agora;

-- name: ExpirarVencidos :execrows
-- Higiene (o roubo não depende disto): lote limitado, sem esperar linhas que
-- uma trava concorrente esteja roubando.
UPDATE holds
SET status = 'expirado', atualizado_em = @agora
WHERE id IN (
    SELECT h.id FROM holds h
    WHERE h.status = 'ativo' AND h.expires_at <= @agora
    ORDER BY h.expires_at
    LIMIT @limite::int
    FOR UPDATE SKIP LOCKED
);
