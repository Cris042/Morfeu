package pedido

import "errors"

// Erros de domínio do módulo pedido, mapeados para HTTP no handler.
var (
	ErrDadosInvalidos       = errors.New("pedido: dados inválidos")
	ErrSessaoIndisponivel   = errors.New("pedido: sessão inexistente, cancelada ou iniciada")
	ErrPedidoPendente       = errors.New("pedido: o carrinho já tem pedido aguardando pagamento")
	ErrHoldsInvalidos       = errors.New("pedido: os assentos não estão travados por este carrinho")
	ErrGatewayIndisponivel  = errors.New("pedido: meio de pagamento indisponível")
	ErrPedidoNaoEncontrado  = errors.New("pedido: inexistente ou de outro dono")
	ErrMuitasRequisicoes    = errors.New("pedido: muitas requisições")
	ErrTransicaoIlegal      = errors.New("pedido: transição ilegal")
	ErrTransicaoConcorrente = errors.New("pedido: outro caminho já transicionou o pedido")
	ErrNaoCancelavel        = errors.New("pedido: só pedido pago, sem ingresso usado, pode ser cancelado")
	ErrForaDaJanela         = errors.New("pedido: cancelamento só até 2h antes da sessão")
)

// ErroValidacao lista os campos inválidos (sem ecoar os valores — o e-mail é PII).
type ErroValidacao struct {
	Campos []string
}

func (e *ErroValidacao) Error() string { return ErrDadosInvalidos.Error() }

// Unwrap permite errors.Is(err, ErrDadosInvalidos).
func (e *ErroValidacao) Unwrap() error { return ErrDadosInvalidos }

// ErroPendente carrega o pedido pendente do carrinho (o SPA retoma por ele).
type ErroPendente struct {
	PedidoID string
}

func (e *ErroPendente) Error() string { return ErrPedidoPendente.Error() }

// Unwrap permite errors.Is(err, ErrPedidoPendente).
func (e *ErroPendente) Unwrap() error { return ErrPedidoPendente }
