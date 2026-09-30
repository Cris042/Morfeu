package pedido

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/pedido/db"
	"github.com/mclovin137/morfeu/internal/pedido/pagamento"
)

// EventoConfirmado é o evento pós-pivô: só o pedido_id (o consumidor busca o
// resto — sem PII no broker, refinamento E6).
const EventoConfirmado = "pedido.confirmado"

// Motivos do estorno automático (ADR 0010).
const (
	MotivoDivergencia = "divergencia" // valor/moeda do pagamento ≠ pedido
	MotivoTardio      = "tardio"      // pagou depois do pedido expirar
	MotivoEmissao     = "emissao"     // assento perdido: não dá para emitir
)

// Resultados do pivô (logs e funil — valores fixos).
const (
	resultadoPago          = "pago"
	resultadoDuplicado     = "duplicado"
	resultadoJaProcessado  = "ja_processado"
	resultadoSemPedido     = "sem_pedido"
	resultadoOutraCobranca = "outra_cobranca"
	EtapaPago              = "pago"
	EtapaPagamentoRecusado = "pagamento_recusado"
	EtapaEstornoNecessario = "estorno_necessario"
	EtapaEstornado         = "estornado"
	EtapaExpirado          = "expirado"
	// Passos de compensação (saga_compensacoes_total{passo}, doc.md §6.1).
	PassoCobranca = "cobranca" // gateway não criou a cobrança → assentos devolvidos
	PassoEstorno  = "estorno"  // pago sem venda possível → dinheiro devolvido
)

// Pagamento é a confirmação de um pagamento aprovado, venha do webhook ou da
// reconciliação (task 0025) — os dois usam o mesmo pivô, protegido pelo CAS.
type Pagamento struct {
	PedidoID      uuid.UUID
	IntencaoID    string
	ValorCentavos int64
	Moeda         string
}

// ProcessarEvento trata um evento já verificado do webhook (RF03). Erro =
// o handler responde 5xx e o Stripe reenvia.
func (s *Servico) ProcessarEvento(ctx context.Context, ev pagamento.EventoPagamento) error {
	switch ev.Tipo {
	case pagamento.PagamentoAprovado:
		return s.aplicar(ctx, ev.ID, string(ev.Tipo), Pagamento{
			PedidoID: ev.PedidoID, IntencaoID: ev.IntencaoID, ValorCentavos: ev.ValorCentavos, Moeda: ev.Moeda,
		})
	case pagamento.PagamentoRecusado:
		// O PaymentIntent volta a aceitar outro cartão: o pedido segue aguardando.
		s.cfg.Funil(ctx, EtapaPagamentoRecusado)
		return nil
	case pagamento.Ignorado:
		return nil
	}
	return nil
}

// AplicarPagamento é o pivô sem evento do webhook (reconciliação, 0025).
func (s *Servico) AplicarPagamento(ctx context.Context, pg Pagamento) error {
	return s.aplicar(ctx, "", "", pg)
}

// aplicar roda o pivô numa TX curta, sem I/O externo (ADR 0010): dedup do
// evento → trava do pedido → cruzamento → emissão ou estorno pendente.
func (s *Servico) aplicar(ctx context.Context, eventoID, tipo string, pg Pagamento) error {
	agora := s.cfg.Agora()
	var resultado string
	err := outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		if eventoID != "" {
			novos, err := db.New(tx).RegistrarEventoStripe(ctx, db.RegistrarEventoStripeParams{EventID: eventoID, Tipo: tipo, Agora: agora})
			if err != nil {
				return fmt.Errorf("pedido: dedup do webhook: %w", err)
			}
			if len(novos) == 0 {
				resultado = resultadoDuplicado
				return nil
			}
		}
		var err error
		resultado, err = s.pivoNaTx(ctx, tx, pg, agora)
		return err
	})
	if err != nil {
		return err
	}
	s.registrarResultado(ctx, pg.PedidoID, resultado)
	return nil
}

func (s *Servico) registrarResultado(ctx context.Context, pedidoID uuid.UUID, resultado string) {
	campos := []zap.Field{zap.String("pedido_id", pedidoID.String()), zap.String("resultado", resultado)}
	switch resultado {
	case resultadoPago:
		s.cfg.Funil(ctx, EtapaPago)
		s.logger.Info("pedido: pagamento confirmado", campos...)
	case MotivoDivergencia, MotivoTardio, MotivoEmissao:
		s.cfg.Funil(ctx, EtapaEstornoNecessario)
		s.logger.Warn("pedido: pagamento exige estorno", campos...)
	case resultadoSemPedido, resultadoOutraCobranca:
		// Dinheiro recebido sem pedido associado: exige olhar humano.
		s.logger.Error("pedido: pagamento sem pedido associado", campos...)
	default:
		s.logger.Info("pedido: evento de pagamento sem efeito", campos...)
	}
}

// pivoNaTx decide o destino do pagamento com a linha do pedido travada.
func (s *Servico) pivoNaTx(ctx context.Context, tx outbox.Tx, pg Pagamento, agora time.Time) (string, error) {
	q := db.New(tx)
	if pg.PedidoID == uuid.Nil {
		return resultadoSemPedido, nil
	}
	linhas, err := q.TravarPedido(ctx, pg.PedidoID)
	if err != nil {
		return "", fmt.Errorf("pedido: travar pedido: %w", err)
	}
	if len(linhas) == 0 {
		return resultadoSemPedido, nil
	}
	p := linhas[0]
	if p.PaymentIntentID != nil && *p.PaymentIntentID != pg.IntencaoID {
		return resultadoOutraCobranca, nil
	}
	if p.PaymentIntentID == nil {
		// Cobrança órfã (a gravação falhou depois do gateway): recupera pelo metadata.
		if _, err := q.DefinirCobranca(ctx, db.DefinirCobrancaParams{PaymentIntentID: &pg.IntencaoID, Agora: agora, ID: p.ID}); err != nil {
			return "", fmt.Errorf("pedido: recuperar cobrança: %w", err)
		}
	}
	st := Status(p.Status)
	r := repositorio{q: q}
	switch {
	case st != AguardandoPagamento && st != Expirado:
		return resultadoJaProcessado, nil
	case pg.ValorCentavos != p.TotalCentavos || !strings.EqualFold(pg.Moeda, Moeda):
		return MotivoDivergencia, r.marcarEstorno(ctx, p.ID, st, MotivoDivergencia, agora)
	case st == Expirado:
		return MotivoTardio, r.marcarEstorno(ctx, p.ID, st, MotivoTardio, agora)
	}
	emitiu, err := s.emitir(ctx, tx, p, agora)
	if err != nil {
		return "", err
	}
	if !emitiu {
		return MotivoEmissao, r.marcarEstorno(ctx, p.ID, st, MotivoEmissao, agora)
	}
	if _, err := r.transicionar(ctx, p.ID, AguardandoPagamento, PagamentoConfirmado, agora); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]string{"pedido_id": p.ID.String()})
	if _, err := outbox.Enqueue(ctx, tx, outbox.Evento{EventType: EventoConfirmado, AggregateID: p.ID.String(), OccurredAt: agora, Payload: payload}); err != nil {
		return "", err
	}
	return resultadoPago, nil
}

// emitir converte os holds do pedido e emite um ingresso por assento num
// savepoint: se algum assento se perdeu (hold roubado depois de vencer) ou já
// tem ingresso ativo, desfaz só o savepoint e devolve false — o pedido vai
// para estorno na mesma TX, sem venda parcial.
func (s *Servico) emitir(ctx context.Context, tx outbox.Tx, p db.TravarPedidoRow, agora time.Time) (bool, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("pedido: savepoint da emissão: %w", err)
	}
	defer func() { _ = sp.Rollback(ctx) }() // no-op depois do Commit
	convertidos, err := s.cfg.Reserva.ConverterDoPedido(ctx, sp, p.ID)
	if err != nil {
		return false, err
	}
	if !mesmoConjunto(convertidos, p.Assentos) {
		return false, nil
	}
	q := db.New(sp)
	for _, a := range p.Assentos {
		ids, err := q.EmitirIngresso(ctx, db.EmitirIngressoParams{ID: uuid.New(), PedidoID: p.ID, SessaoID: p.SessaoID, AssentoCodigo: a, Agora: agora})
		if err != nil {
			return false, fmt.Errorf("pedido: emitir ingresso: %w", err)
		}
		if len(ids) == 0 {
			return false, nil
		}
	}
	if err := sp.Commit(ctx); err != nil {
		return false, fmt.Errorf("pedido: confirmar emissão: %w", err)
	}
	return true, nil
}

func mesmoConjunto(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
