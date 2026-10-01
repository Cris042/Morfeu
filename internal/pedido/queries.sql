-- name: InserirPedido :many
-- Sem linha no RETURNING = o carrinho já tem pedido pendente (índice único
-- parcial pedidos_pendente_por_dono) — decidido pelo banco, sem pré-check.
INSERT INTO pedidos (id, codigo, email, usuario_id, dono_hash, sessao_id, assentos, total_centavos, status, expira_em, criado_em, atualizado_em)
VALUES (@id, @codigo, @email, sqlc.narg('usuario_id'), @dono_hash, @sessao_id, @assentos::varchar[], @total_centavos, 'aguardando_pagamento', @expira_em, @agora, @agora)
ON CONFLICT (dono_hash) WHERE status = 'aguardando_pagamento' DO NOTHING
RETURNING id;

-- name: PendenteDoDono :many
SELECT id, expira_em, payment_intent_id
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
-- Posse (RF06 da 0023; PRD 0031): o carrinho que criou o pedido OU a conta
-- vinculada a ele (usuario_id vem só do JWT).
SELECT id, codigo, sessao_id, assentos::text[] AS assentos, total_centavos, status, expira_em
FROM pedidos
WHERE id = @id
  AND (dono_hash = @dono_hash OR (sqlc.narg('usuario_id')::uuid IS NOT NULL AND usuario_id = sqlc.narg('usuario_id')::uuid));

-- name: ListarPedidosDoUsuario :many
-- "Meus pedidos" (PRD 0031): mais recentes primeiro, paginado.
SELECT id, codigo, sessao_id, assentos::text[] AS assentos, total_centavos, status, expira_em
FROM pedidos
WHERE usuario_id = @usuario_id
ORDER BY criado_em DESC
LIMIT @limite::int OFFSET @deslocamento::int;

-- name: PendenteParaRetomar :many
-- Retomada do pagamento (PRD 0031): só o carrinho dono, com a cobrança.
SELECT status, expira_em, payment_intent_id, total_centavos, codigo
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
-- CAS para estorno_pendente, registrando o motivo (ADR 0010/0011).
UPDATE pedidos
SET status = 'estorno_pendente', motivo_estorno = @motivo, atualizado_em = @agora
WHERE id = @id AND status = @de;

-- name: EmitirIngresso :many
-- Segunda linha de defesa: sem linha = o assento já tem ingresso ativo.
INSERT INTO ingressos (id, pedido_id, sessao_id, assento_codigo, status, versao_token, criado_em)
VALUES (@id, @pedido_id, @sessao_id, @assento_codigo, 'ativo', 1, @agora)
ON CONFLICT (sessao_id, assento_codigo) WHERE status = 'ativo' DO NOTHING
RETURNING id;

-- name: PedidosVencidos :many
-- Reconciliação (PRD 0025): pendentes cujo prazo passou, mais antigos antes.
SELECT id, payment_intent_id
FROM pedidos
WHERE status = 'aguardando_pagamento' AND expira_em <= @agora
ORDER BY expira_em
LIMIT @limite::int;

-- name: EstornosPendentes :many
-- Job de estorno (PRD 0025): o backoff por tentativas é aplicado no serviço.
SELECT id, payment_intent_id, tentativas_estorno, atualizado_em
FROM pedidos
WHERE status = 'estorno_pendente'
ORDER BY atualizado_em
LIMIT @limite::int;

-- name: RegistrarFalhaEstorno :execrows
UPDATE pedidos
SET tentativas_estorno = tentativas_estorno + 1, atualizado_em = @agora
WHERE id = @id AND status = 'estorno_pendente';

-- name: ContarPresos :one
-- Gauge pedidos_presos (PRD 0026): vencidos além da margem do hold (a
-- reconciliação deveria tê-los resolvido) e estornos ainda pendentes.
SELECT
    count(*) FILTER (WHERE status = 'aguardando_pagamento' AND expira_em <= @limite_vencido)::bigint AS aguardando_vencido,
    count(*) FILTER (WHERE status = 'estorno_pendente')::bigint AS estorno_pendente
FROM pedidos
WHERE status IN ('aguardando_pagamento', 'estorno_pendente');

-- name: EncerrarCobranca :exec
-- A cobrança do pedido foi cancelada no gateway (ou já estava): nada a varrer.
UPDATE pedidos SET cobranca_encerrada = true WHERE id = @id;

-- name: ExpiradosComCobrancaAberta :many
-- Varredura (PRD 0027): expirados cuja cobrança não foi encerrada — o
-- cancelamento pode ter sido recusado porque o cliente acabou de pagar.
SELECT id, payment_intent_id
FROM pedidos
WHERE status = 'expirado' AND NOT cobranca_encerrada AND payment_intent_id IS NOT NULL
ORDER BY atualizado_em
LIMIT @limite::int;

-- name: PedidoParaNotificacao :many
-- Porta da notificação (PRD 0028): o e-mail sai só de pedido pago.
SELECT status, email, codigo, sessao_id, total_centavos
FROM pedidos
WHERE id = @id;

-- name: IngressosAtivosDoPedido :many
SELECT id, assento_codigo, versao_token
FROM ingressos
WHERE pedido_id = @pedido_id AND status = 'ativo'
ORDER BY assento_codigo;

-- name: PedidoPorCodigo :many
-- Consulta de convidado (PRD 0034): o código é único; o e-mail é conferido
-- fora do SQL, em tempo constante, pelo mesmo caminho do "não encontrado".
SELECT id, email, codigo, sessao_id, assentos::text[] AS assentos, total_centavos, status, expira_em
FROM pedidos
WHERE codigo = @codigo;

-- name: IngressoParaPagina :many
-- Página pública do ingresso (PRD 0034): o HMAC é conferido no serviço.
SELECT i.assento_codigo, i.status, i.versao_token, i.sessao_id, p.status AS status_pedido
FROM ingressos i
JOIN pedidos p ON p.id = i.pedido_id
WHERE i.id = @id;

-- name: TravarParaCancelar :many
-- Cancelamento (PRD 0036): trava o pedido e diz se algum ingresso já foi usado.
SELECT p.id, p.sessao_id, p.status, p.usuario_id,
       EXISTS (SELECT 1 FROM ingressos i WHERE i.pedido_id = p.id AND i.status = 'usado') AS tem_usado
FROM pedidos p
WHERE p.id = @id
FOR UPDATE OF p;

-- name: CancelarIngressosDoPedido :execrows
-- Ingressos invalidados na TX do cancelamento (ADR 0011): o /i/* vira 410.
UPDATE ingressos SET status = 'cancelado'
WHERE pedido_id = @pedido_id AND status = 'ativo';

-- name: TravarPedidosDaSessao :many
-- Cancelamento da sessão (PRD 0036 RF11): trava TODOS os pedidos da sessão
-- (ordem fixa contra deadlock) — um pivô em curso termina antes.
SELECT id, status
FROM pedidos
WHERE sessao_id = @sessao_id
ORDER BY id
FOR UPDATE;
