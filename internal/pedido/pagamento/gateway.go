// Package pagamento é a borda do pedido com o gateway (ADR 0005 Strategy,
// ADR 0010): a porta Gateway e seus adapters — o fake programável (CI sem
// rede e modo load-test do E12) e, na task 0024, o Stripe. O pedido depende
// só desta interface; a escolha do adapter é do composition root.
package pagamento

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrIndisponivel indica que o gateway não respondeu ou recusou por falha
// dele (rede, 5xx, breaker aberto) — o pedido desfaz a reserva e responde 503.
var ErrIndisponivel = errors.New("pagamento: gateway indisponível")

// Cobranca é o que o pedido pede ao gateway: valor sempre calculado no
// servidor (doc.md §14.2). A chave de idempotência torna o retry seguro.
type Cobranca struct {
	PedidoID          uuid.UUID
	ValorCentavos     int64
	Moeda             string
	ChaveIdempotencia string
}

// Intencao é a cobrança criada: o id fica no pedido (cruzado no webhook) e o
// segredo do cliente vai só para o navegador do dono (Payment Element, E8).
type Intencao struct {
	ID             string
	SegredoCliente string
}

// Gateway é a porta do pedido para o meio de pagamento.
type Gateway interface {
	CriarCobranca(ctx context.Context, c Cobranca) (Intencao, error)
}
