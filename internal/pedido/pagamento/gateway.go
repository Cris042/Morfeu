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

// EstadoCobranca é o que a reconciliação precisa saber de uma cobrança.
type EstadoCobranca string

// Estados da cobrança no gateway.
const (
	CobrancaAprovada  EstadoCobranca = "aprovada"
	CobrancaPendente  EstadoCobranca = "pendente" // aguarda o cliente (ou está processando)
	CobrancaCancelada EstadoCobranca = "cancelada"
)

// Situacao é a cobrança consultada no gateway (reconciliação — task 0025).
type Situacao struct {
	Estado        EstadoCobranca
	ValorCentavos int64
	Moeda         string
}

// Gateway é a porta do pedido para o meio de pagamento. Toda escrita leva
// chave de idempotência: o retry nunca duplica cobrança nem estorno.
type Gateway interface {
	CriarCobranca(ctx context.Context, c Cobranca) (Intencao, error)
	// ConsultarCobranca lê o estado atual (webhook perdido → reconciliação).
	ConsultarCobranca(ctx context.Context, intencaoID string) (Situacao, error)
	// CancelarCobranca impede pagamento futuro de um pedido vencido. Cobrança
	// já aprovada não é cancelada (erro): quem chama reconsulta.
	CancelarCobranca(ctx context.Context, intencaoID string) error
	// Estornar devolve o valor integral da cobrança aprovada.
	Estornar(ctx context.Context, intencaoID, chaveIdempotencia string) error
}
