//go:build integration
// +build integration

// Package checkout_test é o teste de integração ponta a ponta da API do
// checkout (PRD 0026, refinamento E6 §T5 — o E2E com navegador fica no E8):
// trava → pedido → webhook assinado → pivô → outbox → relay → RabbitMQ →
// consumidor de notificação, com PG, Redis e RabbitMQ reais e o gateway fake.
package checkout_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/stripe/stripe-go/v86/webhook"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/broker"
	"github.com/mclovin137/morfeu/internal/catalogo"
	catalogodb "github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/notificacao"
	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/pedido"
	"github.com/mclovin137/morfeu/internal/pedido/pagamento"
	"github.com/mclovin137/morfeu/internal/reserva"
	"github.com/mclovin137/morfeu/internal/sessao"
	sessaodb "github.com/mclovin137/morfeu/internal/sessao/db"
)

const segredoWebhook = "whsec_teste_ponta_a_ponta"

var (
	pool     *pgxpool.Pool
	redisCli *redis.Client
	amqpURL  string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "checkout"},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "postgres:", err)
		os.Exit(1)
	}
	rd, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForLog("Ready to accept connections").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		_ = pg.Terminate(ctx)
		fmt.Fprintln(os.Stderr, "redis:", err)
		os.Exit(1)
	}
	rb, err := rabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine",
		rabbitmq.WithAdminUsername("morfeu"), rabbitmq.WithAdminPassword("morfeu-test-only"),
		testcontainers.WithWaitStrategy(wait.ForLog("Server startup complete").WithStartupTimeout(120*time.Second)))
	if err != nil {
		_ = pg.Terminate(ctx)
		_ = rd.Terminate(ctx)
		fmt.Fprintln(os.Stderr, "rabbitmq:", err)
		os.Exit(1)
	}
	code := func() int {
		if amqpURL, err = rb.AmqpURL(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "amqp url:", err)
			return 1
		}
		rh, _ := rd.Host(ctx)
		rp, _ := rd.MappedPort(ctx, "6379/tcp")
		redisCli = redis.NewClient(&redis.Options{Addr: rh + ":" + rp.Port()})
		defer func() { _ = redisCli.Close() }()
		host, _ := pg.Host(ctx)
		porta, _ := pg.MappedPort(ctx, "5432/tcp")
		if pool, err = pgxpool.New(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%s/checkout?sslmode=disable", host, porta.Port())); err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		defer pool.Close()
		if err := aplicarMigrations(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	_ = pg.Terminate(ctx)
	_ = rd.Terminate(ctx)
	_ = rb.Terminate(ctx)
	os.Exit(code)
}

// aplicarMigrations roda todas as migrations "up" do repositório, em ordem.
func aplicarMigrations(ctx context.Context) error {
	arqs, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(arqs)
	for _, a := range arqs {
		ddl, err := os.ReadFile(a) //nolint:gosec // caminhos fixos do repositório
		if err == nil {
			_, err = pool.Exec(ctx, string(ddl))
		}
		if err != nil {
			return fmt.Errorf("migration %s: %w", a, err)
		}
	}
	return nil
}

// reservaDoPedido espelha o adapter do main (cmd/morfeu).
type reservaDoPedido struct{ s *reserva.Servico }

func (a reservaDoPedido) DonoDoToken(token string) ([]byte, bool) {
	d, ok := reserva.DonoDoToken(token)
	if !ok {
		return nil, false
	}
	return d.Hash(), true
}

func (a reservaDoPedido) PrenderParaPedido(ctx context.Context, tx outbox.Tx, h []byte, sessaoID int64, codigos []string, id uuid.UUID, ate time.Time) error {
	d, ok := reserva.DonoDoHash(h)
	if !ok {
		return pedido.ErrHoldsInvalidos
	}
	err := a.s.PrenderParaPedido(ctx, tx, d, sessaoID, codigos, id, ate)
	if errors.Is(err, reserva.ErrHoldsDoPedido) || errors.Is(err, reserva.ErrDadosInvalidos) {
		return pedido.ErrHoldsInvalidos
	}
	return err
}

func (a reservaDoPedido) LiberarDoPedido(ctx context.Context, tx outbox.Tx, id uuid.UUID) (int64, error) {
	return a.s.LiberarDoPedido(ctx, tx, id)
}

func (a reservaDoPedido) ConverterDoPedido(ctx context.Context, tx outbox.Tx, id uuid.UUID) ([]string, error) {
	return a.s.ConverterDoPedido(ctx, tx, id)
}

// pilha é a aplicação montada como no main, com relay e consumidor rodando.
type pilha struct {
	e         *echo.Echo
	falhar    atomic.Bool // a entrega da notificação falha (e-mail fora)
	entregues sync.Map    // pedido_id → true
	latencias atomic.Int64
}

func montar(t *testing.T) *pilha {
	t.Helper()
	ctx, parar := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() { parar(); wg.Wait() })
	log := zap.NewNop()

	cli := broker.NewClient(amqpURL, log)
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("broker: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	p := &pilha{}
	notif := notificacao.NovoConsumidor(notificacao.Config{
		Latencia: func(context.Context, time.Duration) { p.latencias.Add(1) },
		Entregar: func(_ context.Context, id uuid.UUID) error {
			if p.falhar.Load() {
				return errors.New("e-mail fora do ar")
			}
			p.entregues.Store(id, true)
			return nil
		},
	}, log)
	relay := outbox.NewRelay(pool, cli, log)
	wg.Add(2)
	go func() { defer wg.Done(); relay.Run(ctx) }()
	go func() {
		defer wg.Done()
		_ = cli.Consumir(ctx, broker.QueuePedidoConfirmado, outbox.NovoHandler(pool, notificacao.Consumidor, notif.Efeito, log))
	}()

	limitador := func(nome string) *autenticacao.Limitador {
		l, err := autenticacao.NovoLimitador(autenticacao.ConfigLimitador{Redis: redisCli, Prefixo: "e2e:" + uuid.NewString() + nome, Max: 100000, Janela: time.Minute}, log)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	filmes := catalogo.NovoServico(catalogodb.New(pool), pool, nil, log)
	sessoes, err := sessao.NovoServico(sessaodb.New(pool), sessao.Config{Filmes: filmes}, log)
	if err != nil {
		t.Fatal(err)
	}
	res, err := reserva.NovoServico(pool, reserva.Config{Sessoes: sessoes, LimiteIP: limitador("rip"), LimiteDono: limitador("rdono")}, log)
	if err != nil {
		t.Fatal(err)
	}
	wh, err := pagamento.NovoWebhook(segredoWebhook)
	if err != nil {
		t.Fatal(err)
	}
	ped, err := pedido.NovoServico(pool, pedido.Config{
		Sessoes: sessoes, Reserva: reservaDoPedido{res}, Gateway: pagamento.NovoFake(),
		LimiteIP: limitador("pip"), LimiteDono: limitador("pdono"), Webhook: wh, LimiteWebhook: limitador("wh"),
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	p.e = echo.New()
	reserva.NovoHandler(res, log).RegistrarRotas(p.e)
	pedido.NovoHandler(ped, log).RegistrarRotas(p.e)
	return p
}

func novaSessao(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	var sala, id int64
	if err := pool.QueryRow(ctx, `INSERT INTO salas (nome, layout) VALUES ($1, '{"fileiras":2,"colunas":8,"vaos":[]}') RETURNING id`,
		"Sala "+uuid.NewString()[:8]).Scan(&sala); err != nil {
		t.Fatalf("sala: %v", err)
	}
	inicio := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Minute)
	if err := pool.QueryRow(ctx, `INSERT INTO sessoes (filme_id, sala_id, inicio, duracao_min, fim, preco_centavos)
		VALUES (1, $1, $2, 100, $3, 2500) RETURNING id`, sala, inicio, inicio.Add(2*time.Hour)).Scan(&id); err != nil {
		t.Fatalf("sessão: %v", err)
	}
	return id
}

// req faz a chamada HTTP com o cookie do carrinho e o anti-CSRF.
func (p *pilha) req(t *testing.T, caminho, carrinho string, corpo any) (*httptest.ResponseRecorder, string) {
	t.Helper()
	b, _ := json.Marshal(corpo)
	r := httptest.NewRequest(http.MethodPost, caminho, bytes.NewReader(b))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	r.Header.Set("X-Requested-With", "morfeu")
	if carrinho != "" {
		r.AddCookie(&http.Cookie{Name: "morfeu_carrinho", Value: carrinho}) //nolint:gosec // cookie de requisição no teste
	}
	rec := httptest.NewRecorder()
	p.e.ServeHTTP(rec, r)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "morfeu_carrinho" {
			carrinho = ck.Value
		}
	}
	return rec, carrinho
}

// comprar percorre a jornada pela API até o webhook aprovado.
func (p *pilha) comprar(t *testing.T, assentos ...string) uuid.UUID {
	t.Helper()
	sessaoID := novaSessao(t)
	rec, car := p.req(t, fmt.Sprintf("/sessoes/%d/holds", sessaoID), "", map[string]any{"assentos": assentos})
	if rec.Code != http.StatusCreated {
		t.Fatalf("trava: %d %s", rec.Code, rec.Body)
	}
	rec, _ = p.req(t, "/pedidos", car, map[string]any{"email": "ana@exemplo.com", "sessao_id": sessaoID, "assentos": assentos})
	if rec.Code != http.StatusCreated {
		t.Fatalf("pedido: %d %s", rec.Code, rec.Body)
	}
	var criado struct {
		Pedido struct {
			ID            uuid.UUID `json:"id"`
			TotalCentavos int64     `json:"total_centavos"`
		} `json:"pedido"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &criado)
	evento, _ := json.Marshal(map[string]any{
		"id": "evt_" + uuid.NewString(), "object": "event", "type": "payment_intent.succeeded", "api_version": "2020-01-01",
		"data": map[string]any{"object": map[string]any{
			"id": "pi_fake_" + criado.Pedido.ID.String(), "amount": criado.Pedido.TotalCentavos, "currency": "brl",
			"metadata": map[string]string{"pedido_id": criado.Pedido.ID.String()},
		}},
	})
	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(evento))
	r.Header.Set("Stripe-Signature", webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: evento, Secret: segredoWebhook, Timestamp: time.Now()}).Header)
	w := httptest.NewRecorder()
	p.e.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook: %d %s", w.Code, w.Body)
	}
	return criado.Pedido.ID
}

// eventualmente espera a condição assíncrona (relay 500 ms–1 s + broker), com prazo.
func eventualmente(t *testing.T, prazo time.Duration, cond func() bool, msg string) {
	t.Helper()
	fim := time.Now().Add(prazo)
	for !cond() {
		if time.Now().After(fim) {
			t.Fatal(msg)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func estadoDo(t *testing.T, id uuid.UUID) (status string, ingressos int) {
	t.Helper()
	ctx := context.Background()
	_ = pool.QueryRow(ctx, `SELECT status FROM pedidos WHERE id = $1`, id).Scan(&status)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM ingressos WHERE pedido_id = $1 AND status = 'ativo'`, id).Scan(&ingressos)
	return status, ingressos
}

// TestCheckout_PontaAPonta cobre CA05: da trava à notificação processada.
func TestCheckout_PontaAPonta(t *testing.T) {
	p := montar(t)
	id := p.comprar(t, "A1", "A2")
	if st, n := estadoDo(t, id); st != "pago" || n != 2 {
		t.Fatalf("pivô: %s %d", st, n)
	}
	eventualmente(t, 30*time.Second, func() bool { _, ok := p.entregues.Load(id); return ok }, "notificação não processada")
	var processadas int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM processed_messages pm JOIN outbox_events oe ON oe.id = pm.message_id
		WHERE pm.consumidor = $1 AND oe.aggregate_id = $2`, notificacao.Consumidor, id.String()).Scan(&processadas)
	if processadas != 1 || p.latencias.Load() < 1 {
		t.Fatalf("dedup/latência: processadas=%d latências=%d", processadas, p.latencias.Load())
	}
}

// TestCheckout_NotificacaoNuncaCompensa cobre CA06: a entrega falha sempre →
// redelivery até a DLQ; o pedido segue pago com os ingressos (pós-pivô).
func TestCheckout_NotificacaoNuncaCompensa(t *testing.T) {
	p := montar(t)
	p.falhar.Store(true)
	id := p.comprar(t, "B1")
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	eventualmente(t, 60*time.Second, func() bool {
		q, err := ch.QueueDeclarePassive(broker.QueuePedidoConfirmadoDLQ, true, false, false, false, nil)
		return err == nil && q.Messages >= 1
	}, "mensagem não chegou à DLQ")
	if st, n := estadoDo(t, id); st != "pago" || n != 1 {
		t.Fatalf("a falha da notificação alterou a venda: %s %d", st, n)
	}
	if _, ok := p.entregues.Load(id); ok {
		t.Fatal("não deveria ter entregue")
	}
}
