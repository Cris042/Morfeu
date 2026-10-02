-- name: TravarDono :exec
-- Serializa as TXs de trava do MESMO dono (teto de holds — ADR 0008); solta
-- sozinho no commit/rollback.
SELECT pg_advisory_xact_lock(@namespace::int, @chave::int);

-- name: TentarTravaSweeper :one
-- Uma passada do sweeper por vez entre processos (worker e all).
SELECT pg_try_advisory_xact_lock(@namespace::int, @chave::int) AS obtido;

-- name: HoldsVivosDoDono :many
SELECT id, sessao_id, assento_codigo, expires_at, extensoes_usadas, pedido_id
FROM holds
WHERE dono_hash = @dono_hash AND status = 'ativo' AND expires_at > @agora
ORDER BY expires_at, sessao_id, assento_codigo;

-- name: TravarAssento :many
-- Único caminho de criação (ADR 0008): cria o hold ou rouba um vencido.
-- Sem linha no RETURNING = hold vivo de outro dono → 409.
INSERT INTO holds (id, sessao_id, assento_codigo, dono_hash, status, expires_at, extensoes_usadas, criado_em, atualizado_em)
VALUES (@id, @sessao_id, @assento_codigo, @dono_hash, 'ativo', @expires_at, 0, @agora, @agora)
-- Predicado idêntico ao do índice holds_assento_ocupado (migration 010): um
-- hold 'convertido' (vendido) conflita e NUNCA é roubado; só 'ativo' vencido.
ON CONFLICT (sessao_id, assento_codigo) WHERE status IN ('ativo', 'convertido')
DO UPDATE SET id = EXCLUDED.id, dono_hash = EXCLUDED.dono_hash, expires_at = EXCLUDED.expires_at,
    extensoes_usadas = 0, pedido_id = NULL, criado_em = EXCLUDED.criado_em, atualizado_em = EXCLUDED.atualizado_em
WHERE holds.status = 'ativo' AND holds.expires_at <= @agora
RETURNING id, sessao_id, assento_codigo, expires_at, extensoes_usadas;

-- name: BuscarHoldVivoDoDono :many
SELECT id, sessao_id, assento_codigo, expires_at, extensoes_usadas, pedido_id
FROM holds
WHERE id = @id AND dono_hash = @dono_hash AND status = 'ativo' AND expires_at > @agora;

-- name: EstenderHold :many
-- Guarda da extensão única (última linha de defesa; o aggregate é a primeira).
UPDATE holds
SET expires_at = @novo_expires_at, extensoes_usadas = extensoes_usadas + 1, atualizado_em = @agora
WHERE id = @id AND dono_hash = @dono_hash AND status = 'ativo' AND expires_at > @agora AND extensoes_usadas = 0
    AND pedido_id IS NULL
RETURNING id, sessao_id, assento_codigo, expires_at, extensoes_usadas;

-- name: LiberarHold :execrows
-- Hold preso a um pedido só é liberado pelo pedido (LiberarDoPedido).
UPDATE holds
SET status = 'liberado', atualizado_em = @agora
WHERE id = @id AND dono_hash = @dono_hash AND status = 'ativo' AND expires_at > @agora AND pedido_id IS NULL;

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

-- name: OcupadosDaSessao :many
-- Ocupação pública (PRD 0016/0022): holds vivos + vendidos (convertido não
-- vence), pelo índice único parcial holds_assento_ocupado.
SELECT assento_codigo
FROM holds
WHERE sessao_id = @sessao_id
  AND (status = 'convertido' OR (status = 'ativo' AND expires_at > @agora))
ORDER BY assento_codigo;

-- name: PrenderParaPedido :many
-- Porta do pedido (PRD 0022 RF03): fixa o prazo e o pedido nos holds VIVOS
-- do dono; idempotente para o mesmo pedido. Quem chama confere a cobertura.
UPDATE holds
SET expires_at = @ate, pedido_id = @pedido_id, atualizado_em = @agora
WHERE dono_hash = @dono_hash AND sessao_id = @sessao_id AND assento_codigo = ANY(@codigos::text[])
  AND status = 'ativo' AND expires_at > @agora
  AND (pedido_id IS NULL OR pedido_id = @pedido_id)
RETURNING assento_codigo;

-- name: ConverterDoPedido :execrows
-- Porta do pedido (RF04): vendido. Sem olhar o prazo — se ainda é 'ativo'
-- com este pedido_id, ninguém o roubou (o roubo zera pedido_id).
UPDATE holds
SET status = 'convertido', atualizado_em = @agora
WHERE pedido_id = @pedido_id AND status = 'ativo';

-- name: ConvertidosDoPedido :many
SELECT assento_codigo
FROM holds
WHERE pedido_id = @pedido_id AND status = 'convertido'
ORDER BY assento_codigo;

-- name: LiberarDoPedido :execrows
-- Porta do pedido (RF05): devolve os assentos do pedido — presos ou, no
-- estorno de pedido pago (cancelamento — ADR 0011), já vendidos.
UPDATE holds
SET status = 'liberado', atualizado_em = @agora
WHERE pedido_id = @pedido_id AND status IN ('ativo', 'convertido');

-- name: LimparHoldsTerminais :execrows
-- Limpeza (PRD 0044): só terminais sem efeito (liberado/expirado) com
-- atualizado_em anterior ao corte, em lote. ativo e convertido nunca entram
-- (convertido ocupa o índice da trava). Usa holds_terminais_atualizado_em_idx.
DELETE FROM holds
WHERE id IN (
    SELECT id FROM holds
    WHERE status IN ('liberado', 'expirado') AND atualizado_em < @antes_de::timestamptz
    LIMIT @limite::int
);
