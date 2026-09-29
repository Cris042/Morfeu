-- name: InsertEvent :one
INSERT INTO outbox_events (event_type, aggregate_id, occurred_at, payload)
VALUES ($1, $2, $3, $4)
RETURNING id, event_type, aggregate_id, occurred_at, payload, created_at, published_at;

-- name: SelectPendentesParaPublicar :many
-- Lote de eventos ainda não publicados, travados para esta transação
-- (SKIP LOCKED deixa outras instâncias do relay pularem linhas já em processamento
-- em vez de bloquear — RF03/ADR 0007).
SELECT id, event_type, aggregate_id, occurred_at, payload, created_at, published_at
FROM outbox_events
WHERE published_at IS NULL
ORDER BY created_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarcarPublicado :exec
UPDATE outbox_events
SET published_at = $2
WHERE id = $1;

-- name: ContarPendentes :one
SELECT count(*) FROM outbox_events WHERE published_at IS NULL;

-- name: RegistrarProcessada :execrows
-- Dedup do consumidor idempotente (RF05, task 0005): executada na MESMA TX do
-- efeito de domínio. 0 linhas afetadas = message_id já processado por este
-- consumidor (duplicata) — o efeito não roda. Uma entrega concorrente com o
-- mesmo message_id bloqueia na PK até a primeira TX terminar.
INSERT INTO processed_messages (message_id, consumidor)
VALUES ($1, $2)
ON CONFLICT (message_id, consumidor) DO NOTHING;
