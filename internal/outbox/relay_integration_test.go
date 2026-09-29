//go:build integration
// +build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	dockercontainer "github.com/moby/moby/api/types/container"
	mobynetwork "github.com/moby/moby/api/types/network"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/goleak"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/broker"
	"github.com/mclovin137/morfeu/internal/catalogo"
	catalogodb "github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/outbox"
)

// Suíte de integração do lado produtor (PRD 0002): container por pacote
// (RNF05) — um PG e um RabbitMQ reais compartilhados por todos os testes
// deste arquivo, encerrados em TestMain. goleak.Find() roda depois de
// m.Run() (CA09): usar VerifyTestMain aqui impediria o teardown ordenado dos
// containers (ele chama os.Exit internamente), por isso o Find manual.
var (
	pgDSN        string
	amqpURL      string
	rmqContainer *rabbitmq.RabbitMQContainer
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, dsn, err := setupPostgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup postgres:", err)
		os.Exit(1)
	}
	pgDSN = dsn

	container, url, err := setupRabbitMQ(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup rabbitmq:", err)
		_ = pgContainer.Terminate(ctx)
		os.Exit(1)
	}
	amqpURL = url
	rmqContainer = container

	code := m.Run()

	_ = pgContainer.Terminate(ctx)
	_ = rmqContainer.Terminate(ctx)

	if code == 0 {
		if leakErr := goleak.Find(); leakErr != nil {
			fmt.Fprintln(os.Stderr, "goleak:", leakErr)
			code = 1
		}
	}
	os.Exit(code)
}

// setupPostgres sobe um PG efêmero e aplica as migrations (filmes + outbox_events,
// migrations 001/002; 003/004 lidas dos arquivos) — replica o padrão já usado em internal/integration_test.go.
func setupPostgres(ctx context.Context) (testcontainers.Container, string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "morfeu_outbox_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(120 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("iniciar container postgres: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("host do postgres: %w", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, "", fmt.Errorf("porta do postgres: %w", err)
	}
	dsn := "postgres://postgres:postgres@" + host + ":" + port.Port() + "/morfeu_outbox_test?sslmode=disable"

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, "", fmt.Errorf("pool do postgres: %w", err)
	}
	defer pool.Close()

	// Schema inteiro pelos arquivos de migration reais (tasks 0005/0011): o
	// teste exercita o DDL de produção, sem cópia inline.
	for _, arq := range []string{
		"../../migrations/001_initial_schema.up.sql",
		"../../migrations/002_outbox_events.up.sql",
		"../../migrations/003_processed_messages.up.sql",
		"../../migrations/004_catalogo_filmes_projetados.up.sql",
		"../../migrations/007_filmes.up.sql",
	} {
		ddl, err := os.ReadFile(arq)
		if err != nil {
			return nil, "", fmt.Errorf("ler %s: %w", arq, err)
		}
		if _, err := pool.Exec(ctx, string(ddl)); err != nil {
			return nil, "", fmt.Errorf("aplicar %s: %w", arq, err)
		}
	}

	return container, dsn, nil
}

// setupRabbitMQ sobe um RabbitMQ efêmero (management-alpine, credenciais não
// padrão — RNF01) com wait por log de startup completo (RNF05). A porta 5672
// é publicada numa porta FIXA do host (alocada livre na hora): o stop/start
// do container em CA03 remapeia portas efêmeras (verificado empiricamente),
// e a URL do broker.Client precisa continuar válida após o religamento para
// a reconexão fim-a-fim acontecer de verdade.
func setupRabbitMQ(ctx context.Context) (*rabbitmq.RabbitMQContainer, string, error) {
	hostPort, err := freeHostPort()
	if err != nil {
		return nil, "", fmt.Errorf("alocar porta livre no host: %w", err)
	}

	container, err := rabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine",
		rabbitmq.WithAdminUsername("morfeu"),
		rabbitmq.WithAdminPassword("morfeu-test-only"),
		testcontainers.WithHostConfigModifier(func(hc *dockercontainer.HostConfig) {
			hc.PortBindings = mobynetwork.PortMap{
				// 0.0.0.0: a suíte roda dentro do container golang:1.25
				// (ambiente-dev.md) e alcança o broker via gateway da bridge
				// (172.17.0.1) — bind em loopback não seria alcançável de lá.
				// Exposição curta e com credenciais não-padrão (RNF01).
				mobynetwork.MustParsePort("5672/tcp"): []mobynetwork.PortBinding{
					{HostIP: netip.IPv4Unspecified(), HostPort: hostPort},
				},
			}
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Server startup complete").WithStartupTimeout(120*time.Second),
		),
	)
	if err != nil {
		return nil, "", fmt.Errorf("iniciar container rabbitmq: %w", err)
	}

	url, err := container.AmqpURL(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", fmt.Errorf("amqp url: %w", err)
	}

	return container, url, nil
}

// freeHostPort pede ao kernel uma porta TCP livre e a libera imediatamente —
// há uma janela mínima de corrida até o Docker fazer o bind, aceitável em
// teste (mesma técnica usual para portas fixas com testcontainers).
func freeHostPort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		return "", err
	}
	return strconv.Itoa(port), nil
}

// newTestPool abre um pool próprio por teste sobre o PG compartilhado do
// pacote — mantém os testes independentes entre si mesmo com container único.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgDSN)
	if err != nil {
		t.Fatalf("abrir pool de teste: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// pollUntil tenta cond em intervalos curtos até timeout — substitui sleeps
// fixos (ADR 0006) por espera ativa com condição verificável.
func pollUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cond()
}

// countPending conta outbox_events pendentes com um aggregate_id específico.
func countPending(t *testing.T, pool *pgxpool.Pool, aggregateID string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND published_at IS NULL",
		aggregateID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("contar pendentes: %v", err)
	}
	return count
}

// TestEnqueue_RollbackNaoPersiste cobre CA01/RN01: se a TX que chama Enqueue
// sofrer rollback, o evento nunca persiste na outbox.
func TestEnqueue_RollbackNaoPersiste(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	aggID := uuid.NewString()
	forcedErr := errors.New("erro forçado para testar rollback")

	err := outbox.WithTx(ctx, pool, func(tx outbox.Tx) error {
		if _, enqErr := outbox.Enqueue(ctx, tx, outbox.Evento{
			EventType:   "teste.rollback",
			AggregateID: aggID,
			OccurredAt:  time.Now(),
			Payload:     []byte(`{}`),
		}); enqErr != nil {
			return enqErr
		}
		return forcedErr
	})
	if !errors.Is(err, forcedErr) {
		t.Fatalf("esperava o erro forçado propagado, recebi: %v", err)
	}

	var count int
	if scanErr := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE aggregate_id = $1", aggID).Scan(&count); scanErr != nil {
		t.Fatalf("contar eventos: %v", scanErr)
	}
	if count != 0 {
		t.Fatalf("evento persistiu apesar do rollback da TX: count=%d", count)
	}
}

// TestCreateFilm_FilmEEventoNaMesmaTx cobre CA01/CA05: o service real do
// catálogo cria o filme e o evento catalogo.filme_criado atomicamente.
func TestCreateFilm_FilmEEventoNaMesmaTx(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	svc := catalogo.NovoServico(catalogodb.New(pool), pool, nil, zap.NewNop())

	duracao := int32(100)
	film, err := svc.Criar(ctx, catalogo.DadosFilme{Titulo: "Teste CA01/CA05", DuracaoMin: &duracao})
	if err != nil {
		t.Fatalf("Criar falhou: %v", err)
	}
	if film.ID == 0 {
		t.Fatal("esperava um id de filme válido")
	}

	aggID := fmt.Sprintf("%d", film.ID)
	var eventType string
	var publishedAt *time.Time
	err = pool.QueryRow(ctx,
		"SELECT event_type, published_at FROM outbox_events WHERE aggregate_id = $1", aggID,
	).Scan(&eventType, &publishedAt)
	if err != nil {
		t.Fatalf("evento catalogo.filme_criado não encontrado para o filme criado: %v", err)
	}
	if eventType != "catalogo.filme_criado" {
		t.Errorf("event_type esperado catalogo.filme_criado, recebi %s", eventType)
	}
	if publishedAt != nil {
		t.Errorf("published_at deveria ser NULL logo após o CreateFilm, recebi %v", publishedAt)
	}
}

// TestRelay_PublicaComConfirmEEnvelopeCompleto cobre CA02/RF03/RF05: o relay
// só marca published_at após confirm, e a mensagem chega ao broker com o
// envelope completo (message_id, type, headers, traceparent).
func TestRelay_PublicaComConfirmEEnvelopeCompleto(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zap.NewNop()
	client := broker.NewClient(amqpURL, logger)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("conectar ao broker: %v", err)
	}
	defer func() { _ = client.Close() }()

	aggID := uuid.NewString()
	payload := []byte(`{"hello":"world"}`)
	if err := enqueue(ctx, pool, broker.RoutingKeyFilmeCriado, aggID, payload); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	relay := outbox.NewRelay(pool, client, logger)
	relayCtx, relayCancel := context.WithCancel(ctx)
	defer relayCancel()
	go relay.Run(relayCtx)

	// Consumo cru: drena a fila até achar a mensagem com o aggregate_id
	// desta execução (a fila é compartilhada entre os testes do pacote).
	delivery, err := consumeUntilAggregate(t, aggID, 20*time.Second)
	if err != nil {
		t.Fatalf("mensagem não chegou na fila: %v", err)
	}

	if delivery.Type != broker.RoutingKeyFilmeCriado {
		t.Errorf("Type esperado %s, recebi %s", broker.RoutingKeyFilmeCriado, delivery.Type)
	}
	if delivery.MessageId == "" {
		t.Error("MessageId vazio")
	}
	if delivery.Headers["aggregate_id"] != aggID {
		t.Errorf("header aggregate_id esperado %s, recebi %v", aggID, delivery.Headers["aggregate_id"])
	}
	if delivery.Headers["occurred_at"] == "" || delivery.Headers["occurred_at"] == nil {
		t.Error("header occurred_at ausente")
	}
	tp, _ := delivery.Headers["traceparent"].(string)
	if tp == "" {
		t.Error("header traceparent ausente")
	}
	// Comparação semântica: a coluna payload é JSONB e o Postgres normaliza a
	// serialização no round-trip (ex.: espaço após ":"), então bytes não batem.
	var gotBody, wantBody map[string]any
	if err := json.Unmarshal(delivery.Body, &gotBody); err != nil {
		t.Errorf("body publicado não é JSON válido: %v (body=%s)", err, delivery.Body)
	}
	if err := json.Unmarshal(payload, &wantBody); err != nil {
		t.Fatalf("payload de teste inválido: %v", err)
	}
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Errorf("body esperado %s, recebi %s", payload, delivery.Body)
	}
	if delivery.ContentType != "application/json" {
		t.Errorf("content-type esperado application/json, recebi %s", delivery.ContentType)
	}
	if delivery.DeliveryMode != amqp.Persistent {
		t.Errorf("delivery mode esperado persistent, recebi %d", delivery.DeliveryMode)
	}

	if !pollUntil(t, 10*time.Second, func() bool { return countPending(t, pool, aggID) == 0 }) {
		t.Fatal("published_at não foi marcado após o confirm")
	}
}

// TestOutbox_PendentesContagem cobre RF08: Pendentes reflete o estado antes/
// depois da publicação.
func TestOutbox_PendentesContagem(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	before, err := outbox.Pendentes(ctx, pool)
	if err != nil {
		t.Fatalf("Pendentes (antes): %v", err)
	}

	aggID := uuid.NewString()
	if err := enqueue(ctx, pool, "teste.pendentes", aggID, []byte(`{}`)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	afterEnqueue, err := outbox.Pendentes(ctx, pool)
	if err != nil {
		t.Fatalf("Pendentes (após enqueue): %v", err)
	}
	if afterEnqueue != before+1 {
		t.Fatalf("esperava %d pendentes após enqueue, recebi %d", before+1, afterEnqueue)
	}

	if _, err := pool.Exec(ctx, "UPDATE outbox_events SET published_at = now() WHERE aggregate_id = $1", aggID); err != nil {
		t.Fatalf("marcar publicado manualmente: %v", err)
	}

	afterPublish, err := outbox.Pendentes(ctx, pool)
	if err != nil {
		t.Fatalf("Pendentes (após publicar): %v", err)
	}
	if afterPublish != before {
		t.Fatalf("esperava %d pendentes após publicar, recebi %d", before, afterPublish)
	}
}

// TestBroker_TopologiaIdempotente cobre CA04/RF07: declarar a topologia duas
// vezes consecutivas não produz erro (406 PRECONDITION_FAILED), e a fila
// principal + a DLQ existem.
func TestBroker_TopologiaIdempotente(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zap.NewNop()
	client := broker.NewClient(amqpURL, logger)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.DeclararTopologia(); err != nil {
		t.Fatalf("segunda declaração da topologia falhou: %v", err)
	}

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Fatalf("dial cru para inspeção: %v", err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("canal cru para inspeção: %v", err)
	}
	defer func() { _ = ch.Close() }()

	if _, err := ch.QueueInspect(broker.QueueFilmeCriado); err != nil {
		t.Errorf("fila principal %s não existe: %v", broker.QueueFilmeCriado, err)
	}
	if _, err := ch.QueueInspect(broker.QueueFilmeCriadoDLQ); err != nil {
		t.Errorf("DLQ %s não existe: %v", broker.QueueFilmeCriadoDLQ, err)
	}
}

// TestRelay_BrokerIndisponivelNaoCrashaEReentrega cobre CA03/RF06/RNF06:
// broker parado -> evento fica pendente sem crash; broker religado -> evento
// é publicado (retry/reconexão fim-a-fim).
func TestRelay_BrokerIndisponivelNaoCrashaEReentrega(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zap.NewNop()
	client := broker.NewClient(amqpURL, logger)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = client.Close() }()

	relay := outbox.NewRelay(pool, client, logger)
	relayCtx, relayCancel := context.WithCancel(ctx)
	defer relayCancel()
	go relay.Run(relayCtx)

	aggID := uuid.NewString()
	if err := enqueue(ctx, pool, broker.RoutingKeyFilmeCriado, aggID, []byte(`{"ca03":true}`)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := rmqContainer.Stop(ctx, nil); err != nil {
		t.Fatalf("parar rabbitmq: %v", err)
	}

	// Dá tempo a alguns ciclos de polling falharem sem crash: o evento
	// continua pendente (espera ativa, não sleep fixo).
	stillPending := pollUntil(t, 5*time.Second, func() bool { return countPending(t, pool, aggID) == 1 })
	if !stillPending {
		t.Fatal("evento deveria continuar pendente com o broker fora do ar")
	}

	if err := rmqContainer.Start(ctx); err != nil {
		t.Fatalf("religar rabbitmq: %v", err)
	}

	published := pollUntil(t, 60*time.Second, func() bool { return countPending(t, pool, aggID) == 0 })
	if !published {
		t.Fatal("evento não foi republicado após o broker voltar")
	}
}

// enqueue é um atalho de teste para gravar um evento de outbox fora de um
// caso de uso de domínio real (usado pelos testes que exercitam só o relay).
func enqueue(ctx context.Context, pool *pgxpool.Pool, eventType, aggregateID string, payload []byte) error {
	return outbox.WithTx(ctx, pool, func(tx outbox.Tx) error {
		_, err := outbox.Enqueue(ctx, tx, outbox.Evento{
			EventType:   eventType,
			AggregateID: aggregateID,
			OccurredAt:  time.Now(),
			Payload:     payload,
		})
		return err
	})
}

// consumeUntilAggregate abre um canal cru e drena catalogo.filme_criado até
// achar uma entrega cujo header aggregate_id bate com o esperado — a fila é
// compartilhada entre os testes do pacote, então mensagens de outros casos
// podem aparecer primeiro e são apenas descartadas (autoAck).
func consumeUntilAggregate(t *testing.T, aggregateID string, timeout time.Duration) (amqp.Delivery, error) {
	t.Helper()

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return amqp.Delivery{}, fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return amqp.Delivery{}, fmt.Errorf("channel: %w", err)
	}
	defer func() { _ = ch.Close() }()

	deliveries, err := ch.Consume(broker.QueueFilmeCriado, "", true, false, false, false, nil)
	if err != nil {
		return amqp.Delivery{}, fmt.Errorf("consume: %w", err)
	}

	deadline := time.After(timeout)
	for {
		select {
		case d := <-deliveries:
			if d.Headers["aggregate_id"] == aggregateID {
				return d, nil
			}
		case <-deadline:
			return amqp.Delivery{}, fmt.Errorf("timeout esperando aggregate_id=%s", aggregateID)
		}
	}
}
