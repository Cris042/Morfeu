// Package notificacao é o módulo que entrega o ingresso ao cliente (doc.md
// fluxo crítico 1, ADR 0010): consome pedido.confirmado — pós-pivô, então
// NUNCA compensa a venda: falha → redelivery → DLQ → alerta. Nesta fase
// (task 0026) o consumidor é um stub que registra a confirmação e mede a
// latência pagamento confirmado → notificação; o e-mail com o QR vem no E7.
package notificacao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
)

// Consumidor é o nome do consumidor no registro de dedup (processed_messages).
const Consumidor = "notificacao.pedido_confirmado"

// Config agrupa o que o main injeta (o domínio não conhece OTel).
type Config struct {
	// Latencia recebe o tempo entre o pagamento confirmado (evento) e a
	// notificação processada — o SLI do checkout fim a fim (doc.md §13).
	Latencia func(ctx context.Context, d time.Duration)
	Agora    func() time.Time
	// Entregar é o envio do ingresso; nil = stub (só registra). O E7 injeta o e-mail.
	Entregar func(ctx context.Context, pedidoID uuid.UUID) error
}

// ConsumidorPedidos consome pedido.confirmado.
type ConsumidorPedidos struct {
	cfg    Config
	logger *zap.Logger
}

// NovoConsumidor cria o consumidor com padrões para o que faltar.
func NovoConsumidor(cfg Config, logger *zap.Logger) *ConsumidorPedidos {
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	if cfg.Latencia == nil {
		cfg.Latencia = func(context.Context, time.Duration) {}
	}
	return &ConsumidorPedidos{cfg: cfg, logger: logger}
}

// Efeito processa a mensagem na TX do dedup (outbox.Efeito). Payload sem
// pedido_id válido é permanente (DLQ, sem redelivery). O efeito não toca o
// pedido: a venda já está fechada (ADR 0010).
func (c *ConsumidorPedidos) Efeito(ctx context.Context, _ outbox.Tx, msg outbox.Mensagem) error {
	var p struct {
		PedidoID string `json:"pedido_id"`
	}
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return fmt.Errorf("notificacao: payload ilegível: %w", outbox.ErrPermanente)
	}
	id, err := uuid.Parse(p.PedidoID)
	if err != nil {
		return fmt.Errorf("notificacao: pedido_id inválido: %w", outbox.ErrPermanente)
	}
	if c.cfg.Entregar != nil {
		err := c.cfg.Entregar(ctx, id)
		switch {
		case errors.Is(err, ErrNaoNotificavel):
			// Pedido não pago ou sem ingresso ativo (ex.: estornado): ack sem
			// envio — nenhum replay muda isso e nunca sai QR de ingresso inválido.
			c.logger.Info("notificacao: pedido não notificável, nada enviado", zap.String("pedido_id", id.String()))
			return nil
		case errors.Is(err, ErrPedidoInexistente):
			return fmt.Errorf("notificacao: %w: %w", err, outbox.ErrPermanente)
		case err != nil:
			return fmt.Errorf("notificacao: entregar ingresso: %w", err)
		}
	}
	if !msg.OccurredAt.IsZero() {
		c.cfg.Latencia(ctx, c.cfg.Agora().Sub(msg.OccurredAt))
	}
	c.logger.Info("notificacao: pedido confirmado registrado", zap.String("pedido_id", id.String()))
	return nil
}
