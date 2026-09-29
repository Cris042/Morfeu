package broker

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

const (
	// processamentoTimeout limita uma entrega em curso — inclusive durante o
	// shutdown, quando o handler roda desacoplado do ctx cancelado (RF04).
	processamentoTimeout = 30 * time.Second
	// resubscricaoIntervalo é a espera entre tentativas de reassinar a fila
	// enquanto o Client reconecta (RF03) — sem loop quente.
	resubscricaoIntervalo = 1 * time.Second
	// consumoPrefetch = 1: uma entrega por vez; com nack+requeue o limite de
	// redelivery (x-delivery-limit) segura o custo de mensagem envenenada.
	consumoPrefetch = 1
)

// ErrPermanente marca um erro que não se resolve com nova tentativa (payload
// inválido, message_id ausente): a entrega é rejeitada sem requeue e a quorum
// queue a encaminha direto à DLQ (RF02 do PRD 0005). Qualquer outro erro é
// tratado como transitório (nack com requeue, até x-delivery-limit).
var ErrPermanente = errors.New("broker: erro permanente")

// Entrega é a mensagem recebida (RF01), em tipos próprios do pacote — quem
// consome nunca vê amqp.Delivery (ADR 0003, depguard).
type Entrega struct {
	MessageID   string
	Type        string
	AggregateID string
	Traceparent string
	Body        []byte
	Redelivered bool
}

// Handler processa uma entrega. nil → ack; erro com ErrPermanente → DLQ;
// outro erro → redelivery.
type Handler func(ctx context.Context, e Entrega) error

// Conectado informa se há conexão ativa com o broker (readiness, RF09).
func (c *Client) Conectado() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil && !c.conn.IsClosed()
}

// Consumir assina fila e entrega cada mensagem a h até ctx ser cancelado ou
// o Client ser fechado (RF01). Queda do broker fecha o canal de entregas;
// Consumir espera o Client reconectar (RF06 da 0002) e reassina (RF03).
// Retorna nil no encerramento normal.
func (c *Client) Consumir(ctx context.Context, fila string, h Handler) error {
	for {
		err := c.consumirSessao(ctx, fila, h)
		if ctx.Err() != nil || c.fechado() {
			return nil
		}
		if errors.Is(err, ErrDisconnected) {
			c.logger.Debug("consumer aguardando reconexão ao broker", zap.String("fila", fila))
		} else {
			c.logger.Warn("sessão de consumo encerrada — reassinando", zap.String("fila", fila), zap.Error(err))
		}
		if !sleepWithJitter(ctx, c.done, resubscricaoIntervalo) {
			return nil
		}
	}
}

// consumirSessao abre um canal próprio na conexão atual e consome até o canal
// de entregas fechar (erro) ou a parada ser pedida (nil).
func (c *Client) consumirSessao(ctx context.Context, fila string, h Handler) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		return ErrDisconnected
	}

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("broker: abrir canal de consumo: %w", err)
	}
	// Fechar o canal devolve à fila qualquer entrega não ack'ada (sem perda).
	defer func() {
		if e := ch.Close(); e != nil && !errors.Is(e, amqp.ErrClosed) {
			c.logger.Warn("falha ao fechar canal de consumo", zap.Error(e))
		}
	}()

	if err := ch.Qos(consumoPrefetch, 0, false); err != nil {
		return fmt.Errorf("broker: qos: %w", err)
	}

	tag := fmt.Sprintf("morfeu-%s-%d", fila, time.Now().UnixNano())
	entregas, err := ch.Consume(fila, tag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("broker: assinar fila %s: %w", fila, err)
	}

	for {
		select {
		case <-ctx.Done():
			_ = ch.Cancel(tag, false)
			return nil
		case <-c.done:
			return nil
		case d, ok := <-entregas:
			if !ok {
				return fmt.Errorf("broker: canal de entregas da fila %s fechado", fila)
			}
			c.processar(ctx, d, h)
		}
	}
}

// processar roda h sobre a entrega e faz ack/nack (RF02). O handler recebe um
// ctx desacoplado do cancelamento (RF04): no shutdown a entrega em curso
// termina e é confirmada, limitada por processamentoTimeout.
func (c *Client) processar(ctx context.Context, d amqp.Delivery, h Handler) {
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), processamentoTimeout)
	defer cancel()

	e := Entrega{
		MessageID:   d.MessageId,
		Type:        d.Type,
		AggregateID: headerString(d.Headers, "aggregate_id"),
		Traceparent: headerString(d.Headers, "traceparent"),
		Body:        d.Body,
		Redelivered: d.Redelivered,
	}
	campos := []zap.Field{
		zap.String("event_type", e.Type),
		zap.String("aggregate_id", e.AggregateID),
		zap.String("message_id", e.MessageID),
	}

	err := h(pctx, e)
	switch {
	case err == nil:
		if ackErr := d.Ack(false); ackErr != nil {
			c.logger.Warn("falha no ack — broker reentregará (dedup absorve)", append(campos, zap.Error(ackErr))...)
		}
	case errors.Is(err, ErrPermanente):
		c.logger.Error("mensagem rejeitada sem requeue (DLQ)", append(campos, zap.Error(err))...)
		if nackErr := d.Nack(false, false); nackErr != nil {
			c.logger.Warn("falha no nack", append(campos, zap.Error(nackErr))...)
		}
	default:
		c.logger.Warn("falha transitória — mensagem volta à fila", append(campos, zap.Error(err))...)
		if nackErr := d.Nack(false, true); nackErr != nil {
			c.logger.Warn("falha no nack", append(campos, zap.Error(nackErr))...)
		}
	}
}

// fechado informa se Close já foi chamado.
func (c *Client) fechado() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func headerString(h amqp.Table, chave string) string {
	if v, ok := h[chave].(string); ok {
		return v
	}
	return ""
}
