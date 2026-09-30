-- name: InserirPedido :many
-- Sem linha no RETURNING = o carrinho já tem pedido pendente (índice único
-- parcial pedidos_pendente_por_dono) — decidido pelo banco, sem pré-check.
INSERT INTO pedidos (id, codigo, email, dono_hash, sessao_id, assentos, total_centavos, status, expira_em, criado_em, atualizado_em)
VALUES (@id, @codigo, @email, @dono_hash, @sessao_id, @assentos::varchar[], @total_centavos, 'aguardando_pagamento', @expira_em, @agora, @agora)
ON CONFLICT (dono_hash) WHERE status = 'aguardando_pagamento' DO NOTHING
RETURNING id;

-- name: PendenteDoDono :many
SELECT id, expira_em
FROM pedidos
WHERE dono_hash = @dono_hash AND status = 'aguardando_pagamento';

-- name: Transicionar :execrows
-- Compare-and-swap (ADR 0005/0010): só transiciona a partir do estado lido.
-- Zero linhas = outro caminho já transicionou (corrida perdida).
UPDATE pedidos
SET status = @para, atualizado_em = @agora
WHERE id = @id AND status = @de;

-- name: RegistrarEvento :exec
-- Trilha append-only das transições (só IDs e estados).
INSERT INTO pedido_eventos (pedido_id, de, para, ocorrido_em)
VALUES (@pedido_id, sqlc.narg('de'), @para, @agora);

-- name: DefinirCobranca :execrows
-- Grava o id da cobrança do gateway enquanto o pedido espera pagamento.
UPDATE pedidos
SET payment_intent_id = @payment_intent_id, atualizado_em = @agora
WHERE id = @id AND status = 'aguardando_pagamento' AND payment_intent_id IS NULL;

-- name: BuscarPedidoDoDono :many
-- Posse (RF06): só o carrinho que criou o pedido o enxerga.
SELECT id, codigo, sessao_id, assentos::text[] AS assentos, total_centavos, status, expira_em
FROM pedidos
WHERE id = @id AND dono_hash = @dono_hash;
