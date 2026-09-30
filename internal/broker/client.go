// Package broker é a única fronteira do projeto que importa amqp091-go
// (ADR 0003, RNF03 do PRD 0002 — enforced por depguard em .golangci.yml).
// Encapsula conexão, reconexão manual (NotifyClose + backoff exponencial),
// declaração idempotente da topologia (RF07) e publish com confirm síncrono
// (RF03), conforme ADR 0007.
package broker

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// Nomes da topologia (RF07): exchange topic única + quorum queue com DLQ.
// Parâmetros imutáveis em código — dois startups consecutivos não geram 406
// (CA04), pois DeclararTopologia é sempre chamada com os mesmos argumentos.
const (
	ExchangeEvents        = "morfeu.events"
	ExchangeEventsDLX     = "morfeu.events.dlx"
	QueueFilmeCriado      = "catalogo.filme_criado"
	QueueFilmeCriadoDLQ   = "catalogo.filme_criado.dlq"
	RoutingKeyFilmeCriado = "catalogo.filme_criado"

	// Pedido confirmado → notificação (PRD 0026, ADR 0010). DLX própria: a DLX
	// de filmes é fanout e mandaria as mortas de uma fila para a DLQ da outra.
	QueuePedidoConfirmado       = "notificacao.pedido_confirmado"
	QueuePedidoConfirmadoDLQ    = "notificacao.pedido_confirmado.dlq"
	ExchangePedidoConfirmadoDLX = "notificacao.pedido_confirmado.dlx"
	RoutingKeyPedidoConfirmado  = "pedido.confirmado"

	// Pedido estornado → aviso ao cliente (PRD 0030), com DLX/DLQ próprias.
	QueuePedidoEstornado       = "notificacao.pedido_estornado"
	QueuePedidoEstornadoDLQ    = "notificacao.pedido_estornado.dlq"
	ExchangePedidoEstornadoDLX = "notificacao.pedido_estornado.dlx"
	RoutingKeyPedidoEstornado  = "pedido.estornado"

	filmeCriadoDeliveryLimit = int32(3)

	confirmTimeout = 5 * time.Second
	backoffBase    = 1 * time.Second
	backoffCap     = 30 * time.Second
)

// ErrDisconnected indica que não há conexão ativa com o broker no momento do
// publish — o relay trata isso como falha transitória (evento fica pendente,
// RNF06/RF06: nenhum crash, nenhuma perda).
var ErrDisconnected = errors.New("broker: sem conexão ativa com o RabbitMQ")

// ErrNaoConfirmado indica que o broker não confirmou (ack) a publicação
// dentro do timeout — RF03: published_at só é gravado após confirm positivo.
var ErrNaoConfirmado = errors.New("broker: publish não confirmado pelo broker")

// Message é o envelope de publicação (RF05): tipos próprios do pacote broker,
// nunca amqp.Publishing cru, para que quem publica (internal/outbox/relay.go)
// não precise importar amqp091-go.
type Message struct {
	RoutingKey  string
	Body        []byte
	MessageID   string
	Type        string
	AggregateID string
	OccurredAt  time.Time
	Traceparent string
}

// Client é a conexão única por processo com o RabbitMQ (RF06): reconexão
// manual via NotifyClose + backoff exponencial (base 1s, teto 30s, jitter).
// Durante desconexão, PublicarComConfirm retorna ErrDisconnected — o relay
// segue rodando e tentando de novo no próximo ciclo (nenhum crash).
type Client struct {
	url    string
	logger *zap.Logger

	mu   sync.RWMutex
	conn *amqp.Connection
	ch   *amqp.Channel

	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
}

// NewClient cria um cliente ainda não conectado — chame Start para dial +
// declarar a topologia e manter a conexão viva em segundo plano.
func NewClient(url string, logger *zap.Logger) *Client {
	return &Client{
		url:    url,
		logger: logger,
		done:   make(chan struct{}),
	}
}

// Start conecta, declara a topologia (RF07) e inicia a goroutine de
// keep-alive/reconexão. Bloqueia só o dial inicial; a partir daí retorna e a
// reconexão roda em segundo plano até ctx ser cancelado ou Close ser chamado
// (RF04: shutdown por ctx, sem goroutine órfã).
func (c *Client) Start(ctx context.Context) error {
	if err := c.connect(ctx); err != nil {
		return err
	}

	c.wg.Add(1)
	go c.keepAlive(ctx)
	return nil
}

// Close para a goroutine de keep-alive e fecha canal/conexão. Idempotente.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
	})
	c.wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()

	var err error
	if c.ch != nil {
		if e := c.ch.Close(); e != nil && !errors.Is(e, amqp.ErrClosed) {
			err = e
		}
	}
	if c.conn != nil {
		if e := c.conn.Close(); e != nil && !errors.Is(e, amqp.ErrClosed) {
			err = e
		}
	}
	return err
}

// DeclararTopologia (re)declara a topologia completa com a conexão atual.
// Chamada implicitamente a cada (re)conexão; exposta para os testes de CA04
// (declaração 2x consecutivas sem 406 PRECONDITION_FAILED).
func (c *Client) DeclararTopologia() error {
	c.mu.RLock()
	ch := c.ch
	c.mu.RUnlock()

	if ch == nil {
		return ErrDisconnected
	}
	return declareTopology(ch)
}

// PublicarComConfirm publica msg na exchange morfeu.events com routing key =
// msg.RoutingKey e aguarda o publisher confirm síncrono (RF03): timeout de
// 5s via PublishWithDeferredConfirm + WaitContext. Broker indisponível ou
// confirm negado/timeout → erro (RN02: o chamador NÃO marca published_at).
func (c *Client) PublicarComConfirm(ctx context.Context, msg Message) error {
	c.mu.RLock()
	ch := c.ch
	c.mu.RUnlock()

	if ch == nil {
		return ErrDisconnected
	}

	pub := amqp.Publishing{
		Headers: amqp.Table{
			"aggregate_id": msg.AggregateID,
			"occurred_at":  msg.OccurredAt.UTC().Format(time.RFC3339),
			"traceparent":  msg.Traceparent,
		},
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    msg.MessageID,
		Type:         msg.Type,
		Timestamp:    msg.OccurredAt.UTC(),
		Body:         msg.Body,
	}

	dc, err := ch.PublishWithDeferredConfirm(ExchangeEvents, msg.RoutingKey, false, false, pub)
	if err != nil {
		return fmt.Errorf("broker: publish falhou: %w", err)
	}
	if dc == nil {
		// Channel não está em modo confirm — não deveria acontecer (Confirm
		// é chamado em connect()), mas é um erro de programação, não de rede.
		return fmt.Errorf("broker: canal não está em modo confirm")
	}

	waitCtx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()

	ok, err := dc.WaitContext(waitCtx)
	if err != nil {
		return fmt.Errorf("broker: aguardando confirm: %w", err)
	}
	if !ok {
		return ErrNaoConfirmado
	}
	return nil
}

// ProfundidadeFila devolve quantas mensagens prontas a fila tem (declaração
// passiva num canal efêmero — não cria nem altera a fila). Fonte do gauge
// morfeu_dlq_mensagens (RF04 do PRD 0006).
func (c *Client) ProfundidadeFila(nome string) (int, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		return 0, ErrDisconnected
	}

	ch, err := conn.Channel()
	if err != nil {
		return 0, fmt.Errorf("broker: abrir canal de inspeção: %w", err)
	}
	defer func() { _ = ch.Close() }()

	q, err := ch.QueueDeclarePassive(nome, true, false, false, false, nil)
	if err != nil {
		return 0, fmt.Errorf("broker: inspecionar fila %s: %w", nome, err)
	}
	return q.Messages, nil
}

// connect faz o dial, abre um canal em modo confirm e declara a topologia.
func (c *Client) connect(_ context.Context) error {
	conn, err := amqp.DialConfig(c.url, amqp.Config{
		Heartbeat: 10 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("broker: dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("broker: abrir canal: %w", err)
	}

	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return fmt.Errorf("broker: habilitar modo confirm: %w", err)
	}

	if err := declareTopology(ch); err != nil {
		_ = conn.Close()
		return fmt.Errorf("broker: declarar topologia: %w", err)
	}

	c.mu.Lock()
	c.conn, c.ch = conn, ch
	c.mu.Unlock()

	return nil
}

// keepAlive observa NotifyClose da conexão atual e reconecta com backoff
// exponencial (RF06) até ctx ser cancelado ou Close ser chamado.
func (c *Client) keepAlive(ctx context.Context) {
	defer c.wg.Done()

	for {
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		closeErr := conn.NotifyClose(make(chan *amqp.Error, 1))

		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case err, open := <-closeErr:
			if !open || err == nil {
				// Fechamento intencional (Close() já em andamento).
				return
			}
			// Zera o canal/conexão antes de tentar reconectar: enquanto
			// desconectado, PublicarComConfirm deve falhar com ErrDisconnected
			// (evento fica pendente, RNF06) em vez de usar um canal morto.
			c.mu.Lock()
			c.ch = nil
			c.conn = nil
			c.mu.Unlock()

			c.logger.Warn("conexão com o broker perdida — reconectando",
				zap.Error(err),
			)
			if !c.reconnectWithBackoff(ctx) {
				return
			}
			c.logger.Info("reconectado ao broker")
		}
	}
}

// reconnectWithBackoff tenta reconectar indefinidamente com backoff
// exponencial (base 1s, teto 30s, jitter) até suceder ou ctx/done sinalizar
// parada. Retorna false se a parada foi solicitada antes de reconectar —
// nenhum crash, o relay que consome este client segue apenas falhando o
// publish (RNF06).
func (c *Client) reconnectWithBackoff(ctx context.Context) bool {
	backoff := backoffBase
	for {
		select {
		case <-ctx.Done():
			return false
		case <-c.done:
			return false
		default:
		}

		err := c.connect(ctx)
		if err == nil {
			return true
		}
		c.logger.Warn("falha ao reconectar ao broker, nova tentativa em breve",
			zap.Duration("backoff", backoff),
			zap.Error(err),
		)

		if !sleepWithJitter(ctx, c.done, backoff) {
			return false
		}
		backoff *= 2
		if backoff > backoffCap {
			backoff = backoffCap
		}
	}
}

// sleepWithJitter dorme d + até 20% de jitter, retornando false se ctx ou
// done sinalizarem antes do fim do sono (permite shutdown imediato).
func sleepWithJitter(ctx context.Context, done <-chan struct{}, d time.Duration) bool {
	jitter := time.Duration(0)
	if n, err := rand.Int(rand.Reader, big.NewInt(int64(d)/5+1)); err == nil {
		jitter = time.Duration(n.Int64())
	}

	timer := time.NewTimer(d + jitter)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-done:
		return false
	case <-timer.C:
		return true
	}
}

// declareTopology declara idempotentemente a exchange topic, a DLX (fanout),
// a DLQ e a quorum queue com x-delivery-limit=3 (RF07). Argumentos fixos:
// redeclarar com os mesmos parâmetros nunca produz 406 PRECONDITION_FAILED.
func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(ExchangeEvents, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("exchange %s: %w", ExchangeEvents, err)
	}

	if err := ch.ExchangeDeclare(ExchangeEventsDLX, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		return fmt.Errorf("exchange %s: %w", ExchangeEventsDLX, err)
	}

	if _, err := ch.QueueDeclare(QueueFilmeCriadoDLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("fila %s: %w", QueueFilmeCriadoDLQ, err)
	}
	if err := ch.QueueBind(QueueFilmeCriadoDLQ, "", ExchangeEventsDLX, false, nil); err != nil {
		return fmt.Errorf("bind %s -> %s: %w", QueueFilmeCriadoDLQ, ExchangeEventsDLX, err)
	}

	queueArgs := amqp.Table{
		"x-queue-type":           "quorum",
		"x-delivery-limit":       filmeCriadoDeliveryLimit,
		"x-dead-letter-exchange": ExchangeEventsDLX,
	}
	if _, err := ch.QueueDeclare(QueueFilmeCriado, true, false, false, false, queueArgs); err != nil {
		return fmt.Errorf("fila %s: %w", QueueFilmeCriado, err)
	}
	if err := ch.QueueBind(QueueFilmeCriado, RoutingKeyFilmeCriado, ExchangeEvents, false, nil); err != nil {
		return fmt.Errorf("bind %s -> %s: %w", QueueFilmeCriado, ExchangeEvents, err)
	}

	if err := declararFilaComDLQ(ch, QueuePedidoConfirmado, RoutingKeyPedidoConfirmado, ExchangePedidoConfirmadoDLX, QueuePedidoConfirmadoDLQ); err != nil {
		return err
	}
	return declararFilaComDLQ(ch, QueuePedidoEstornado, RoutingKeyPedidoEstornado, ExchangePedidoEstornadoDLX, QueuePedidoEstornadoDLQ)
}

// declararFilaComDLQ declara uma quorum queue ligada à morfeu.events com a
// própria DLX fanout → DLQ (mesmos parâmetros imutáveis da fila de filmes).
func declararFilaComDLQ(ch *amqp.Channel, fila, routingKey, dlx, dlq string) error {
	if err := ch.ExchangeDeclare(dlx, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		return fmt.Errorf("exchange %s: %w", dlx, err)
	}
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("fila %s: %w", dlq, err)
	}
	if err := ch.QueueBind(dlq, "", dlx, false, nil); err != nil {
		return fmt.Errorf("bind %s -> %s: %w", dlq, dlx, err)
	}
	args := amqp.Table{
		"x-queue-type":           "quorum",
		"x-delivery-limit":       filmeCriadoDeliveryLimit,
		"x-dead-letter-exchange": dlx,
	}
	if _, err := ch.QueueDeclare(fila, true, false, false, false, args); err != nil {
		return fmt.Errorf("fila %s: %w", fila, err)
	}
	if err := ch.QueueBind(fila, routingKey, ExchangeEvents, false, nil); err != nil {
		return fmt.Errorf("bind %s -> %s: %w", fila, ExchangeEvents, err)
	}
	return nil
}
