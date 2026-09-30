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
-- Grava o id da cobrança uma única vez. Sem filtro de status: o pivô também
-- recupera a cobrança órfã (gateway respondeu, mas a gravação falhou) de um
-- pedido já expirado, que precisa dela para o estorno (auditoria 0023).
UPDATE pedidos
SET payment_intent_id = @payment_intent_id, atualizado_em = @agora
WHERE id = @id AND payment_intent_id IS NULL;

-- name: BuscarPedidoDoDono :many
-- Posse (RF06): só o carrinho que criou o pedido o enxerga.
SELECT id, codigo, sessao_id, assentos::text[] AS assentos, total_centavos, status, expira_em
FROM pedidos
WHERE id = @id AND dono_hash = @dono_hash;

-- name: RegistrarEventoStripe :many
-- Dedup do webhook na TX do efeito: sem linha no RETURNING = já processado.
INSERT INTO stripe_eventos (event_id, tipo, recebido_em)
VALUES (@event_id, @tipo, @agora)
ON CONFLICT (event_id) DO NOTHING
RETURNING event_id;

-- name: TravarPedido :many
-- Pivô (ADR 0010): a linha do pedido fica travada até o fim da TX.
SELECT id, sessao_id, assentos::text[] AS assentos, total_centavos, status, payment_intent_id
FROM pedidos
WHERE id = @id
FOR UPDATE;

-- name: MarcarEstorno :execrows
-- CAS para estorno_pendente, registrando o motivo (divergencia|tardio|emissao).
UPDATE pedidos
SET status = 'estorno_pendente', motivo_estorno = @motivo, atualizado_em = @agora
WHERE id = @id AND status = @de;

-- name: EmitirIngresso :many
-- Segunda linha de defesa: sem linha = o assento já tem ingresso ativo.
INSERT INTO ingressos (id, pedido_id, sessao_id, assento_codigo, status, versao_token, criado_em)
VALUES (@id, @pedido_id, @sessao_id, @assento_codigo, 'ativo', 1, @agora)
ON CONFLICT (sessao_id, assento_codigo) WHERE status = 'ativo' DO NOTHING
RETURNING id;
