package pedido

import "fmt"

// Status é o estado do pedido na saga do checkout (ADR 0010). O pivô é
// aguardando_pagamento → pago; o estorno é a única compensação — também
// para o cancelamento (ADR 0011: sem estado "cancelado", o motivo distingue).
type Status string

// Estados persistidos (CHECK da migration 011).
const (
	AguardandoPagamento Status = "aguardando_pagamento"
	Pago                Status = "pago"
	Expirado            Status = "expirado"
	Falhou              Status = "falhou"
	EstornoPendente     Status = "estorno_pendente"
	Estornado           Status = "estornado"
)

// Evento é o fato que pede uma transição.
type Evento string

// Eventos da máquina.
const (
	PagamentoConfirmado Evento = "pagamento_confirmado" // webhook/reconciliação (0024/0025)
	CobrancaFalhou      Evento = "cobranca_falhou"      // gateway não criou a cobrança
	PrazoVencido        Evento = "prazo_vencido"        // expira_em passou sem pagamento
	EstornoNecessario   Evento = "estorno_necessario"   // pago sem poder emitir (divergência, tardio, conflito)
	EstornoConcluido    Evento = "estorno_concluido"    // gateway confirmou o estorno
	// CancelamentoSolicitado: cliente, operador ou sessão cancelada (ADR 0011).
	CancelamentoSolicitado Evento = "cancelamento_solicitado"
)

// transicoes é a tabela da máquina (State idiomático — ADR 0005). Tudo que
// não está aqui é ilegal; terminais (falhou, estornado) e pago não aceitam
// nada; pago só sai por cancelamento (ADR 0011).
var transicoes = map[Status]map[Evento]Status{
	AguardandoPagamento: {
		PagamentoConfirmado: Pago,
		CobrancaFalhou:      Falhou,
		PrazoVencido:        Expirado,
		EstornoNecessario:   EstornoPendente,
	},
	// Pagamento que chega depois do prazo: o cliente foi cobrado sem pedido
	// válido → estorno automático (ADR 0010, motivo "tardio").
	Expirado:        {EstornoNecessario: EstornoPendente},
	Pago:            {CancelamentoSolicitado: EstornoPendente},
	EstornoPendente: {EstornoConcluido: Estornado},
}

// ErroTransicao é uma transição fora da tabela.
type ErroTransicao struct {
	De     Status
	Evento Evento
}

func (e *ErroTransicao) Error() string {
	return fmt.Sprintf("pedido: transição ilegal: %s + %s", e.De, e.Evento)
}

// Unwrap permite errors.Is(err, ErrTransicaoIlegal).
func (e *ErroTransicao) Unwrap() error { return ErrTransicaoIlegal }

// Transicionar devolve o estado de destino ou ErroTransicao. A garantia sob
// concorrência é o compare-and-swap no SQL (repositório); esta função
// documenta e testa a máquina (primeira linha de defesa).
func Transicionar(de Status, ev Evento) (Status, error) {
	if para, ok := transicoes[de][ev]; ok {
		return para, nil
	}
	return "", &ErroTransicao{De: de, Evento: ev}
}
