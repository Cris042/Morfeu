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

// Nomes dos consumidores no registro de dedup (processed_messages).
const (
	Consumidor        = "notificacao.pedido_confirmado"
	ConsumidorEstorno = "notificacao.pedido_estornado" // PRD 0030
)

// Config agrupa o que o main injeta (o domínio não conhece OTel).
type Config struct {
	// Tipo do e-mail (label `tipo`): TipoConfirmacao (padrão) ou TipoEstorno.
	Tipo string
	// Latencia recebe o tempo entre o pagamento confirmado (evento) e a
	// notificação processada — o SLI do checkout fim a fim (doc.md §13).
	Latencia func(ctx context.Context, d time.Duration)
	Agora    func() time.Time
	// Entregar é o envio do ingresso; nil = stub (só registra). O E7 injeta o e-mail.
	Entregar func(ctx context.Context, pedidoID uuid.UUID) error
	// Resultado recebe cada tentativa de entrega (tipo, resultado, duração) —
	// morfeu_email_* (PRD 0029). Resultados: ok, ignorado, transitorio,
	// permanente, cota.
	Resultado func(ctx context.Context, tipo, resultado string, d time.Duration)
}

// Resultados de uma entrega (label `resultado` — lista fechada).
const (
	ResultadoOK          = "ok"
	ResultadoIgnorado    = "ignorado"
	ResultadoTransitorio = "transitorio"
	ResultadoPermanente  = "permanente"
	ResultadoCota        = "cota"
)

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
	if cfg.Tipo == "" {
		cfg.Tipo = TipoConfirmacao
	}
	if cfg.Resultado == nil {
		cfg.Resultado = func(context.Context, string, string, time.Duration) {}
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
		if err := c.entregar(ctx, id); errors.Is(err, errIgnorado) {
			return nil
		} else if err != nil {
			return err
		}
	}
	if !msg.OccurredAt.IsZero() {
		c.cfg.Latencia(ctx, c.cfg.Agora().Sub(msg.OccurredAt))
	}
	c.logger.Info("notificacao: pedido notificado", zap.String("tipo", c.cfg.Tipo), zap.String("pedido_id", id.String()))
	return nil
}

// entregar roda a entrega, registra o resultado e decide a classe do erro:
// não notificável = ack sem envio; inexistente, recusa e cota = permanente
// (DLQ); o resto volta à fila (redelivery → DLQ após 3).
func (c *ConsumidorPedidos) entregar(ctx context.Context, id uuid.UUID) error {
	inicio := c.cfg.Agora()
	err := c.cfg.Entregar(ctx, id)
	resultado := ResultadoOK
	switch {
	case errors.Is(err, ErrNaoNotificavel):
		resultado = ResultadoIgnorado
		err = nil
		// Pedido não pago ou sem ingresso ativo (ex.: estornado): nenhum
		// replay muda isso e nunca sai QR de ingresso inválido.
		c.logger.Info("notificacao: pedido não notificável, nada enviado", zap.String("pedido_id", id.String()))
	case errors.Is(err, ErrCotaEsgotada):
		resultado = ResultadoCota
		err = fmt.Errorf("notificacao: %w: %w", err, outbox.ErrPermanente)
	case errors.Is(err, ErrPedidoInexistente), errors.Is(err, ErrEnvioPermanente):
		resultado = ResultadoPermanente
		err = fmt.Errorf("notificacao: %w: %w", err, outbox.ErrPermanente)
	case err != nil:
		resultado = ResultadoTransitorio
		err = fmt.Errorf("notificacao: entregar ingresso: %w", err)
	}
	c.cfg.Resultado(ctx, c.cfg.Tipo, resultado, c.cfg.Agora().Sub(inicio))
	if resultado == ResultadoIgnorado {
		return errIgnorado
	}
	return err
}

// errIgnorado sinaliza ao Efeito que não houve envio (ack sem latência).
var errIgnorado = errors.New("notificacao: ignorado")
