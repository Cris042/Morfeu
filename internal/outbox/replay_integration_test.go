//go:build integration
// +build integration

package outbox_test

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/broker"
	"github.com/mclovin137/morfeu/internal/outbox"
)

// Replay da DLQ, panic no handler e limpeza da outbox (PRD 0027). Reusa o
// TestMain/containers do relay_integration_test.go; usa a topologia real da
// fila de filmes (o pacote não roda em paralelo — mesmo guard-rail do CA05).

func clienteTeste(t *testing.T) *broker.Client {
	t.Helper()
	c := broker.NewClient(amqpURL, zap.NewNop())
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("conectar ao broker: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func limparFilasDeFilmes(t *testing.T, ch *amqp.Channel) {
	t.Helper()
	for _, f := range []string{broker.QueueFilmeCriado, broker.QueueFilmeCriadoDLQ} {
		if _, err := ch.QueuePurge(f, false); err != nil {
			t.Fatalf("purge %s: %v", f, err)
		}
	}
}

// morta publica direto na DLQ uma mensagem como o dead-letter a deixaria,
// com o evento ocorrido agora (dentro da janela do dedup).
func morta(t *testing.T, ch *amqp.Channel, id string) {
	t.Helper()
	mortaEm(t, ch, id, time.Now())
}

// mortaEm é morta com o instante do evento (Timestamp AMQP) escolhido.
func mortaEm(t *testing.T, ch *amqp.Channel, id string, evento time.Time) {
	t.Helper()
	err := ch.PublishWithContext(context.Background(), "", broker.QueueFilmeCriadoDLQ, false, false, amqp.Publishing{
		MessageId: id, Timestamp: evento, Type: "catalogo.filme_criado", ContentType: "application/json", DeliveryMode: amqp.Persistent,
		Headers: amqp.Table{"aggregate_id": "42", "x-death": amqp.Table{"count": int64(3)}}, Body: []byte(`{"id":42}`),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func profundidade(t *testing.T, ch *amqp.Channel, fila string) int {
	t.Helper()
	q, err := ch.QueueDeclarePassive(fila, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("inspecionar %s: %v", fila, err)
	}
	return q.Messages
}

func corteReplay() time.Time { return time.Now().Add(-outbox.JanelaDedup) }

// TestReplayDLQ_RetemMensagemAlemDaJanela cobre CA04 (PRD 0044): evento mais
// velho que a janela do dedup fica na DLQ (retida, sem ack nem publish); o
// mais novo é republicado; sem Timestamp também é retido (opção segura).
func TestReplayDLQ_RetemMensagemAlemDaJanela(t *testing.T) {
	cli := clienteTeste(t)
	ch := canalTeste(t)
	limparFilasDeFilmes(t, ch)
	corte := corteReplay()
	nova := uuid.NewString()
	mortaEm(t, ch, uuid.NewString(), corte.Add(-time.Second)) // velha
	mortaEm(t, ch, nova, corte.Add(time.Second))              // dentro da janela
	mortaEm(t, ch, uuid.NewString(), time.Time{})             // sem timestamp
	pollUntil(t, 5*time.Second, func() bool { return profundidade(t, ch, broker.QueueFilmeCriadoDLQ) == 3 })

	// Dry-run conta as retidas sem publicar.
	r, err := cli.Reprocessar(context.Background(), broker.QueueFilmeCriadoDLQ, 10, true, corte)
	if err != nil || r.Lidas != 3 || r.Retidas != 2 || r.Republicadas != 0 {
		t.Fatalf("dry-run: %+v %v", r, err)
	}
	if !pollUntil(t, 5*time.Second, func() bool { return profundidade(t, ch, broker.QueueFilmeCriadoDLQ) == 3 }) {
		t.Fatal("dry-run deveria devolver tudo à DLQ")
	}

	r, err = cli.Reprocessar(context.Background(), broker.QueueFilmeCriadoDLQ, 10, false, corte)
	if err != nil || r.Lidas != 3 || r.Retidas != 2 || r.Republicadas != 1 {
		t.Fatalf("replay: %+v %v", r, err)
	}
	if !pollUntil(t, 5*time.Second, func() bool {
		return profundidade(t, ch, broker.QueueFilmeCriado) == 1 && profundidade(t, ch, broker.QueueFilmeCriadoDLQ) == 2
	}) {
		t.Fatal("esperado 1 republicada e 2 retidas na DLQ")
	}
	d, ok, err := ch.Get(broker.QueueFilmeCriado, true)
	if err != nil || !ok || d.MessageId != nova {
		t.Fatalf("republicada errada: ok=%v id=%s err=%v", ok, d.MessageId, err)
	}
	limparFilasDeFilmes(t, ch)
}

// TestReplayDLQ cobre CA01–CA03: dry-run não publica e devolve; replay
// republica na fila de origem com o MESMO message_id e esvazia a DLQ até o
// limite; fila fora da lista é recusada.
func TestReplayDLQ(t *testing.T) {
	cli := clienteTeste(t)
	ch := canalTeste(t)
	limparFilasDeFilmes(t, ch)
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, id := range ids {
		morta(t, ch, id)
	}
	pollUntil(t, 5*time.Second, func() bool { return profundidade(t, ch, broker.QueueFilmeCriadoDLQ) == 3 })

	r, err := cli.Reprocessar(context.Background(), broker.QueueFilmeCriadoDLQ, 10, true, corteReplay())
	if err != nil || r.Lidas != 3 || r.Republicadas != 0 {
		t.Fatalf("dry-run: %+v %v", r, err)
	}
	if !pollUntil(t, 5*time.Second, func() bool { return profundidade(t, ch, broker.QueueFilmeCriadoDLQ) == 3 }) {
		t.Fatal("dry-run deveria devolver as mensagens à DLQ")
	}
	if profundidade(t, ch, broker.QueueFilmeCriado) != 0 {
		t.Fatal("dry-run publicou")
	}

	r, err = cli.Reprocessar(context.Background(), broker.QueueFilmeCriadoDLQ, 2, false, corteReplay())
	if err != nil || r.Lidas != 2 || r.Republicadas != 2 {
		t.Fatalf("replay: %+v %v", r, err)
	}
	if !pollUntil(t, 5*time.Second, func() bool {
		return profundidade(t, ch, broker.QueueFilmeCriado) == 2 && profundidade(t, ch, broker.QueueFilmeCriadoDLQ) == 1
	}) {
		t.Fatal("limite não respeitado ou mensagens não republicadas")
	}
	vistos := map[string]bool{}
	for range 2 {
		d, ok, err := ch.Get(broker.QueueFilmeCriado, true)
		// A ordem do requeue do dry-run não é garantida: basta serem 2 dos 3 ids.
		if err != nil || !ok || !slices.Contains(ids, d.MessageId) || vistos[d.MessageId] || d.Headers["aggregate_id"] != "42" {
			t.Fatalf("mensagem republicada: ok=%v id=%s headers=%v err=%v", ok, d.MessageId, d.Headers, err)
		}
		vistos[d.MessageId] = true
		if _, temXDeath := d.Headers["x-death"]; temXDeath {
			t.Fatal("x-death não deveria ser copiado (a contagem recomeça)")
		}
	}

	if _, err := cli.Reprocessar(context.Background(), "qualquer.fila", 1, false, corteReplay()); !errors.Is(err, broker.ErrFilaNaoPermitida) {
		t.Fatalf("fila fora da lista: %v", err)
	}
	limparFilasDeFilmes(t, ch)
}

// TestConsumir_PanicVaiParaDLQ cobre CA04: um handler que entra em panic não
// derruba o consumidor — a entrega vai à DLQ e a próxima é processada.
func TestConsumir_PanicVaiParaDLQ(t *testing.T) {
	cli := clienteTeste(t)
	ch := canalTeste(t)
	limparFilasDeFilmes(t, ch)
	ctx, cancel := context.WithCancel(context.Background())
	feito := make(chan struct{})
	var processou atomic.Bool
	go func() {
		defer close(feito)
		_ = cli.Consumir(ctx, broker.QueueFilmeCriado, func(_ context.Context, e broker.Entrega) error {
			if e.MessageID == "panico" {
				panic("bug no handler")
			}
			processou.Store(true)
			return nil
		})
	}()
	defer func() { cancel(); <-feito }()
	for _, id := range []string{"panico", uuid.NewString()} {
		if err := ch.PublishWithContext(context.Background(), broker.ExchangeEvents, broker.RoutingKeyFilmeCriado, false, false,
			amqp.Publishing{MessageId: id, Body: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if !pollUntil(t, 10*time.Second, func() bool { return profundidade(t, ch, broker.QueueFilmeCriadoDLQ) == 1 && processou.Load() }) {
		t.Fatal("panic não foi para a DLQ ou o consumidor parou")
	}
	limparFilasDeFilmes(t, ch)
}

// TestLimparPublicados cobre CA05: só publicados além da retenção saem;
// pendentes nunca.
func TestLimparPublicados(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	inserir := func(publicado *time.Time) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO outbox_events (event_type, aggregate_id, occurred_at, payload, published_at)
			VALUES ('teste.limpeza', 'x', now(), '{}', $1) RETURNING id`, publicado).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	velho, recente := time.Now().Add(-10*24*time.Hour), time.Now().Add(-24*time.Hour)
	apagar := []uuid.UUID{inserir(&velho), inserir(&velho)}
	manter := []uuid.UUID{inserir(&recente), inserir(nil)}
	if _, err := outbox.LimparPublicados(ctx, pool, outbox.RetencaoPublicadosDias); err != nil {
		t.Fatal(err)
	}
	contar := func(ids []uuid.UUID) int {
		var n int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE id = ANY($1)`, ids).Scan(&n)
		return n
	}
	if contar(apagar) != 0 || contar(manter) != 2 {
		t.Fatalf("restaram apagáveis=%d mantidos=%d", contar(apagar), contar(manter))
	}
}
