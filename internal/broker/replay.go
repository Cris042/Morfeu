package broker

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// filaDeOrigem de cada DLQ: conferida (declaração passiva) antes do lote.
var filaDeOrigem = map[string]string{
	QueueFilmeCriadoDLQ:      QueueFilmeCriado,
	QueuePedidoConfirmadoDLQ: QueuePedidoConfirmado,
	QueuePedidoEstornadoDLQ:  QueuePedidoEstornado,
}

// FilasReplay lista as DLQs aceitas pelo replay e a routing key da
// fila de origem de cada uma (PRD 0027) — lista fechada: nada de publicar em
// routing key vinda do operador.
var FilasReplay = map[string]string{
	QueueFilmeCriadoDLQ:      RoutingKeyFilmeCriado,
	QueuePedidoConfirmadoDLQ: RoutingKeyPedidoConfirmado,
	QueuePedidoEstornadoDLQ:  RoutingKeyPedidoEstornado,
}

// ErrFilaNaoPermitida: DLQ fora de FilasReplay.
var ErrFilaNaoPermitida = errors.New("broker: fila fora da lista de replay")

// ResultadoReplay resume uma execução (só ids — nunca payload).
type ResultadoReplay struct {
	Lidas        int
	Republicadas int
	// Retidas: não republicadas porque o evento é mais velho que o corte do
	// dedup (PRD 0044); continuam na DLQ para decisão manual.
	Retidas    int
	MessageIDs []string
}

// Reprocessar devolve até limite mensagens da DLQ à exchange morfeu.events com
// a routing key da fila de origem (ADR 0007: replay adiado para o E6). Cada
// mensagem é republicada com confirm ANTES do ack na DLQ: queda no meio
// duplica (o dedup do consumidor absorve), nunca perde. O message_id é
// preservado — é a chave do dedup. Em dry-run nada é publicado nem
// confirmado: as mensagens lidas voltam à DLQ quando o canal fecha.
//
// corte é o instante a partir do qual o dedup do consumidor ainda vale
// (agora − janela do dedup): mensagem com Timestamp anterior ao corte — ou sem
// Timestamp, opção segura — NÃO é republicada nem confirmada (conta em
// Retidas), pois o registro de dedup dela pode ter sido limpo e o efeito
// repetiria. Retidas não consomem o limite.
func (c *Client) Reprocessar(ctx context.Context, dlq string, limite int, dryRun bool, corte time.Time) (ResultadoReplay, error) {
	var out ResultadoReplay
	routingKey, ok := FilasReplay[dlq]
	if !ok {
		return out, fmt.Errorf("%w: %s", ErrFilaNaoPermitida, dlq)
	}
	if limite < 1 {
		return out, errors.New("broker: limite do replay deve ser >= 1")
	}
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		return out, ErrDisconnected
	}
	ch, err := conn.Channel()
	if err != nil {
		return out, fmt.Errorf("broker: abrir canal de replay: %w", err)
	}
	// Fechar o canal devolve à DLQ tudo que não foi ack'ado (dry-run e erro).
	defer func() { _ = ch.Close() }()
	// Sem a fila de origem, o publish seria confirmado e descartado (sem
	// rota) e a mensagem sairia da DLQ para lugar nenhum (auditoria 0027).
	if _, err := ch.QueueDeclarePassive(filaDeOrigem[dlq], true, false, false, false, nil); err != nil {
		return out, fmt.Errorf("broker: fila de origem de %s indisponível: %w", dlq, err)
	}
	if err := ch.Confirm(false); err != nil {
		return out, fmt.Errorf("broker: confirm no canal de replay: %w", err)
	}
	err = moverLote(ctx, ch, dlq, routingKey, limite, dryRun, corte, &out)
	c.logger.Info("replay da DLQ", zap.String("fila", dlq), zap.Bool("dry_run", dryRun),
		zap.Int("lidas", out.Lidas), zap.Int("republicadas", out.Republicadas), zap.Int("retidas", out.Retidas), zap.Strings("message_ids", out.MessageIDs))
	return out, err
}

// moverLote lê até limite mensagens da DLQ e, fora do dry-run, republica cada
// uma com confirm antes do ack.
func moverLote(ctx context.Context, ch *amqp.Channel, dlq, routingKey string, limite int, dryRun bool, corte time.Time, out *ResultadoReplay) error {
	for out.Lidas-out.Retidas < limite {
		d, ok, err := ch.Get(dlq, false)
		if err != nil {
			return fmt.Errorf("broker: ler %s: %w", dlq, err)
		}
		if !ok {
			return nil
		}
		out.Lidas++
		out.MessageIDs = append(out.MessageIDs, d.MessageId)
		if d.Timestamp.IsZero() || d.Timestamp.Before(corte) {
			// Sem ack: a entrega volta à DLQ quando o canal fecha.
			out.Retidas++
			continue
		}
		if dryRun {
			continue
		}
		if err := republicar(ctx, ch, routingKey, d); err != nil {
			return err
		}
		if err := d.Ack(false); err != nil {
			// Já republicada: a cópia na DLQ volta e seria reprocessada de novo
			// — o dedup do consumidor absorve.
			return fmt.Errorf("broker: ack na DLQ: %w", err)
		}
		out.Republicadas++
	}
	return nil
}

// republicar copia a entrega morta (propriedades e headers de negócio) para a
// exchange de eventos e espera o confirm. x-death e afins ficam de fora: a
// mensagem recomeça a contagem de entregas na fila de origem.
func republicar(ctx context.Context, ch *amqp.Channel, routingKey string, d amqp.Delivery) error {
	headers := amqp.Table{}
	for _, k := range []string{"aggregate_id", "occurred_at", "traceparent"} {
		if v, ok := d.Headers[k]; ok {
			headers[k] = v
		}
	}
	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, ExchangeEvents, routingKey, false, false, amqp.Publishing{
		Headers: headers, ContentType: d.ContentType, DeliveryMode: amqp.Persistent,
		MessageId: d.MessageId, Type: d.Type, Timestamp: d.Timestamp, Body: d.Body,
	})
	if err != nil {
		return fmt.Errorf("broker: republicar %s: %w", d.MessageId, err)
	}
	cctx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()
	confirmado, err := dc.WaitContext(cctx)
	if err != nil {
		return fmt.Errorf("broker: confirm do replay: %w", err)
	}
	if !confirmado {
		return ErrNaoConfirmado
	}
	return nil
}
