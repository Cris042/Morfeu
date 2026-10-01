package reserva

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/reserva/db"
)

// Portas transacionais do pedido (PRD 0022, ADR 0010): o service do pedido é
// o dono da TX e a repassa. São os ÚNICOS métodos da reserva que recebem uma
// transação alheia — o pedido não importa este pacote (a porta é declarada no
// consumidor e ligada no main).

// PrenderParaPedido fixa o prazo `ate` e o pedido nos holds vivos do dono
// naquela sessão (RF03). Idempotente para o mesmo pedido; se os holds não
// cobrem todos os códigos, devolve ErrHoldsDoPedido — quem chamou desfaz a TX.
func (s *Servico) PrenderParaPedido(ctx context.Context, tx outbox.Tx, d Dono, sessaoID int64, codigos []string, pedidoID uuid.UUID, ate time.Time) error {
	lote, err := NovoLote(codigos)
	if err != nil {
		return err
	}
	agora := s.cfg.Agora()
	if !ate.After(agora) {
		return fmt.Errorf("reserva: prazo do pedido no passado: %w", ErrDadosInvalidos)
	}
	presos, err := db.New(tx).PrenderParaPedido(ctx, db.PrenderParaPedidoParams{
		Ate: ate, PedidoID: &pedidoID, Agora: agora, DonoHash: d.hash, SessaoID: sessaoID, Codigos: lote.codigos(),
	})
	if err != nil {
		return fmt.Errorf("reserva: prender para o pedido: %w", err)
	}
	if len(presos) != len(lote.assentos) {
		return ErrHoldsDoPedido
	}
	s.logger.Info("reserva: holds presos ao pedido", zap.Int64("sessao_id", sessaoID), zap.Int("quantidade", len(presos)))
	return nil
}

// ConverterDoPedido marca como vendidos os holds do pedido e devolve TODOS os
// códigos convertidos dele, inclusive os de uma chamada anterior (RF04 —
// idempotente). Menos códigos que o esperado = algum hold foi roubado depois
// de vencer: quem chama decide (estorno, ADR 0010).
func (s *Servico) ConverterDoPedido(ctx context.Context, tx outbox.Tx, pedidoID uuid.UUID) ([]string, error) {
	q := db.New(tx)
	n, err := q.ConverterDoPedido(ctx, db.ConverterDoPedidoParams{Agora: s.cfg.Agora(), PedidoID: &pedidoID})
	if err != nil {
		return nil, fmt.Errorf("reserva: converter holds do pedido: %w", err)
	}
	codigos, err := q.ConvertidosDoPedido(ctx, &pedidoID)
	if err != nil {
		return nil, fmt.Errorf("reserva: convertidos do pedido: %w", err)
	}
	if n > 0 {
		// Dentro da TX de quem chamou: um rollback posterior superestima a
		// métrica em no máximo um lote — aceitável para um contador.
		s.cfg.Metricas.Convertidos(ctx, n)
	}
	return codigos, nil
}

// LiberarDoPedido devolve os assentos do pedido (RF05): os presos e, quando o
// pedido pago é estornado (cancelamento — ADR 0011), os vendidos. Só é
// chamado na expiração/falha (nada vendido) e no estornado.
func (s *Servico) LiberarDoPedido(ctx context.Context, tx outbox.Tx, pedidoID uuid.UUID) (int64, error) {
	n, err := db.New(tx).LiberarDoPedido(ctx, db.LiberarDoPedidoParams{Agora: s.cfg.Agora(), PedidoID: &pedidoID})
	if err != nil {
		return 0, fmt.Errorf("reserva: liberar holds do pedido: %w", err)
	}
	return n, nil
}
