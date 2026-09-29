//go:build integration
// +build integration

package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/broker"
	"github.com/mclovin137/morfeu/internal/catalogo"
	catalogodb "github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/outbox"
)

// Suíte do lado consumidor (PRD 0005). Reusa o TestMain/containers do
// relay_integration_test.go. Cenários de dedup/DLQ usam uma fila própria por
// teste (mesmos args da topologia real: quorum + x-delivery-limit=3 + DLX)
// para isolamento; o fim a fim (CA05) usa a topologia real.

type filaTeste struct {
	fila string
	dlq  string
}

// canalTeste abre conexão/canal AMQP crus (permitidos em teste — depguard
// exclui _test) para declarar filas, publicar e inspecionar.
func canalTeste(t *testing.T) *amqp.Channel {
	t.Helper()
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Fatalf("dial amqp: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		t.Fatalf("canal amqp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return ch
}

func declararFilaTeste(t *testing.T, ch *amqp.Channel) filaTeste {
	t.Helper()
	sufixo := uuid.NewString()[:8]
	dlx, dlq, fila := "teste.dlx."+sufixo, "teste.dlq."+sufixo, "teste.fila."+sufixo

	if err := ch.ExchangeDeclare(dlx, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		t.Fatalf("declarar dlx: %v", err)
	}
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		t.Fatalf("declarar dlq: %v", err)
	}
	if err := ch.QueueBind(dlq, "", dlx, false, nil); err != nil {
		t.Fatalf("bind dlq: %v", err)
	}
	if _, err := ch.QueueDeclare(fila, true, false, false, false, amqp.Table{
		"x-queue-type":           "quorum",
		"x-delivery-limit":       int32(3),
		"x-dead-letter-exchange": dlx,
	}); err != nil {
		t.Fatalf("declarar fila: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ch.QueueDelete(fila, false, false, false)
		_, _ = ch.QueueDelete(dlq, false, false, false)
		_ = ch.ExchangeDelete(dlx, false, false)
	})
	return filaTeste{fila: fila, dlq: dlq}
}

func publicarNaFila(t *testing.T, ch *amqp.Channel, fila, messageID string, body []byte) {
	t.Helper()
	err := ch.PublishWithContext(context.Background(), "", fila, false, false, amqp.Publishing{
		MessageId:    messageID,
		Type:         "catalogo.filme_criado",
		Headers:      amqp.Table{"aggregate_id": "teste"},
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
	if err != nil {
		t.Fatalf("publicar: %v", err)
	}
}

func mensagensProntas(t *testing.T, ch *amqp.Channel, fila string) int {
	t.Helper()
	q, err := ch.QueueDeclarePassive(fila, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("inspecionar fila %s: %v", fila, err)
	}
	return q.Messages
}

// iniciarConsumer sobe broker.Client + Consumir e registra o shutdown
// ordenado no Cleanup. Devolve o cancel para testes que exercitam shutdown
// e o canal fechado quando Consumir retorna.
func iniciarConsumer(t *testing.T, fila string, h broker.Handler) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	client := broker.NewClient(amqpURL, zap.NewNop())
	if err := client.Start(ctx); err != nil {
		cancel()
		t.Fatalf("conectar ao broker: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := client.Consumir(ctx, fila, h); err != nil {
			t.Errorf("Consumir retornou erro: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = client.Close()
	})
	return cancel, done
}

func novoFilmID() int64 {
	return 1_000_000 + rand.Int64N(1_000_000_000)
}

func payloadFilme(id int64) []byte {
	return []byte(fmt.Sprintf(`{"id":%d,"title":"Filme %d","year":2024}`, id, id))
}

// aplicacoes lê a coluna de prova de idempotência da projeção (-1 se ausente).
func aplicacoes(t *testing.T, pool *pgxpool.Pool, filmID int64) int32 {
	t.Helper()
	p, err := catalogodb.New(pool).BuscarFilmeProjetado(context.Background(), filmID)
	if err != nil {
		return -1
	}
	return p.Aplicacoes
}

func contarProcessadas(t *testing.T, pool *pgxpool.Pool, messageID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM processed_messages WHERE message_id = $1", messageID,
	).Scan(&n); err != nil {
		t.Fatalf("contar processed_messages: %v", err)
	}
	return n
}

// contando envolve h contando as chamadas.
func contando(h broker.Handler, n *atomic.Int32) broker.Handler {
	return func(ctx context.Context, e broker.Entrega) error {
		n.Add(1)
		return h(ctx, e)
	}
}

// TestConsumidor_MensagemDuplicadaAplicaUmaVez cobre CA01/RN01: a mesma
// mensagem entregue 2x produz o efeito 1x e ambas são ack'adas.
func TestConsumidor_MensagemDuplicadaAplicaUmaVez(t *testing.T) {
	pool := newTestPool(t)
	ch := canalTeste(t)
	ft := declararFilaTeste(t, ch)

	var chamadas atomic.Int32
	h := outbox.NovoHandler(pool, catalogo.ConsumidorProjecaoFilmes, catalogo.ProjetarFilmeCriado, zap.NewNop())
	iniciarConsumer(t, ft.fila, contando(h, &chamadas))

	filmID, msgID := novoFilmID(), uuid.NewString()
	publicarNaFila(t, ch, ft.fila, msgID, payloadFilme(filmID))
	publicarNaFila(t, ch, ft.fila, msgID, payloadFilme(filmID))

	if !pollUntil(t, 15*time.Second, func() bool { return chamadas.Load() == 2 }) {
		t.Fatalf("esperava 2 entregas processadas, recebi %d", chamadas.Load())
	}
	if got := aplicacoes(t, pool, filmID); got != 1 {
		t.Errorf("efeito deveria ser aplicado 1x, aplicacoes=%d", got)
	}
	if n := contarProcessadas(t, pool, msgID); n != 1 {
		t.Errorf("processed_messages deveria ter 1 linha, tem %d", n)
	}
	if !pollUntil(t, 5*time.Second, func() bool { return mensagensProntas(t, ch, ft.fila) == 0 }) {
		t.Error("fila deveria estar vazia (ambas ack'adas)")
	}
	if n := mensagensProntas(t, ch, ft.dlq); n != 0 {
		t.Errorf("DLQ deveria estar vazia, tem %d", n)
	}
}

// TestConsumidor_FalhaAntesDoCommitDesfazEfeitoEDedup cobre CA02: erro após o
// efeito desfaz projeção E registro de dedup na mesma TX; a redelivery aplica.
func TestConsumidor_FalhaAntesDoCommitDesfazEfeitoEDedup(t *testing.T) {
	pool := newTestPool(t)
	ch := canalTeste(t)
	ft := declararFilaTeste(t, ch)

	var tentativas atomic.Int32
	efeito := func(ctx context.Context, tx outbox.Tx, msg outbox.Mensagem) error {
		if err := catalogo.ProjetarFilmeCriado(ctx, tx, msg); err != nil {
			return err
		}
		if tentativas.Add(1) == 1 {
			return errors.New("falha injetada após o efeito, antes do commit")
		}
		return nil
	}
	iniciarConsumer(t, ft.fila, outbox.NovoHandler(pool, catalogo.ConsumidorProjecaoFilmes, efeito, zap.NewNop()))

	filmID, msgID := novoFilmID(), uuid.NewString()
	publicarNaFila(t, ch, ft.fila, msgID, payloadFilme(filmID))

	if !pollUntil(t, 15*time.Second, func() bool { return aplicacoes(t, pool, filmID) >= 1 }) {
		t.Fatal("redelivery não aplicou o efeito")
	}
	if got := tentativas.Load(); got != 2 {
		t.Errorf("esperava 2 tentativas (falha + sucesso), recebi %d", got)
	}
	if got := aplicacoes(t, pool, filmID); got != 1 {
		t.Errorf("1ª tentativa deveria ter sido desfeita: aplicacoes=%d", got)
	}
	if n := contarProcessadas(t, pool, msgID); n != 1 {
		t.Errorf("processed_messages deveria ter 1 linha, tem %d", n)
	}
}

// TestConsumidor_PayloadMalformadoVaiParaDLQ cobre CA03/RN02/RNF03: mensagem
// envenenada (payload ilegível ou sem message_id) vai à DLQ e a fila
// principal segue consumindo.
func TestConsumidor_PayloadMalformadoVaiParaDLQ(t *testing.T) {
	pool := newTestPool(t)
	ch := canalTeste(t)
	ft := declararFilaTeste(t, ch)

	iniciarConsumer(t, ft.fila, outbox.NovoHandler(pool, catalogo.ConsumidorProjecaoFilmes, catalogo.ProjetarFilmeCriado, zap.NewNop()))

	malformadaID := uuid.NewString()
	publicarNaFila(t, ch, ft.fila, malformadaID, []byte(`{"id":`))
	publicarNaFila(t, ch, ft.fila, "", payloadFilme(novoFilmID()))
	filmID := novoFilmID()
	publicarNaFila(t, ch, ft.fila, uuid.NewString(), payloadFilme(filmID))

	if !pollUntil(t, 15*time.Second, func() bool { return aplicacoes(t, pool, filmID) == 1 }) {
		t.Fatal("mensagem válida após as envenenadas não foi projetada — fila principal travou")
	}
	if !pollUntil(t, 10*time.Second, func() bool { return mensagensProntas(t, ch, ft.dlq) == 2 }) {
		t.Fatalf("DLQ deveria ter 2 mensagens, tem %d", mensagensProntas(t, ch, ft.dlq))
	}
	if n := contarProcessadas(t, pool, malformadaID); n != 0 {
		t.Errorf("mensagem rejeitada não pode ficar registrada como processada (%d)", n)
	}

	d, ok, err := ch.Get(ft.dlq, true)
	if err != nil || !ok {
		t.Fatalf("ler DLQ: ok=%v err=%v", ok, err)
	}
	if d.MessageId != malformadaID {
		t.Errorf("1ª mensagem da DLQ deveria ser a malformada %s, recebi %q", malformadaID, d.MessageId)
	}
}

// TestConsumidor_ErroTransitorioEsgotaDeliveryLimit cobre CA04: falha
// transitória persistente é reentregue até x-delivery-limit e termina na DLQ.
func TestConsumidor_ErroTransitorioEsgotaDeliveryLimit(t *testing.T) {
	ch := canalTeste(t)
	ft := declararFilaTeste(t, ch)

	var chamadas atomic.Int32
	iniciarConsumer(t, ft.fila, func(context.Context, broker.Entrega) error {
		chamadas.Add(1)
		return errors.New("dependência indisponível")
	})

	publicarNaFila(t, ch, ft.fila, uuid.NewString(), payloadFilme(novoFilmID()))

	if !pollUntil(t, 20*time.Second, func() bool { return mensagensProntas(t, ch, ft.dlq) == 1 }) {
		t.Fatalf("mensagem deveria ter ido à DLQ após o limite; chamadas=%d", chamadas.Load())
	}
	if got := chamadas.Load(); got < 3 {
		t.Errorf("esperava ao menos 3 entregas antes da DLQ (x-delivery-limit=3), recebi %d", got)
	}
}

// TestConsumidor_ShutdownTerminaEntregaEmCurso cobre CA06/RF04: com uma
// entrega em processamento, cancelar o ctx não interrompe o efeito — ele
// conclui, é ack'ado (fila vazia) e Consumir retorna sem goroutine órfã.
func TestConsumidor_ShutdownTerminaEntregaEmCurso(t *testing.T) {
	pool := newTestPool(t)
	ch := canalTeste(t)
	ft := declararFilaTeste(t, ch)

	iniciou := make(chan struct{})
	liberar := make(chan struct{})
	efeito := func(ctx context.Context, tx outbox.Tx, msg outbox.Mensagem) error {
		close(iniciou)
		<-liberar
		return catalogo.ProjetarFilmeCriado(ctx, tx, msg)
	}
	cancel, done := iniciarConsumer(t, ft.fila, outbox.NovoHandler(pool, catalogo.ConsumidorProjecaoFilmes, efeito, zap.NewNop()))

	filmID := novoFilmID()
	publicarNaFila(t, ch, ft.fila, uuid.NewString(), payloadFilme(filmID))

	select {
	case <-iniciou:
	case <-time.After(15 * time.Second):
		t.Fatal("entrega não começou a ser processada")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("Consumir retornou com entrega ainda em processamento")
	case <-time.After(200 * time.Millisecond):
	}
	close(liberar)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Consumir não retornou após o shutdown")
	}
	if got := aplicacoes(t, pool, filmID); got != 1 {
		t.Errorf("efeito em curso deveria concluir 1x no shutdown, aplicacoes=%d", got)
	}
	if n := mensagensProntas(t, ch, ft.fila); n != 0 {
		t.Errorf("entrega concluída deveria estar ack'ada (fila vazia), tem %d", n)
	}
}

// TestWalkingSkeleton_CriarFilmeAteProjecao cobre CA05: CreateFilm (service
// real) → outbox → relay → broker (topologia real) → consumer → projeção.
func TestWalkingSkeleton_CriarFilmeAteProjecao(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := broker.NewClient(amqpURL, zap.NewNop())
	if err := client.Start(ctx); err != nil {
		t.Fatalf("conectar ao broker: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Topologia real: descarta sobras de testes do produtor na fila.
	if _, err := canalTeste(t).QueuePurge(broker.QueueFilmeCriado, false); err != nil {
		t.Fatalf("purge da fila real: %v", err)
	}

	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		outbox.NewRelay(pool, client, zap.NewNop()).Run(ctx)
	}()
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		h := outbox.NovoHandler(pool, catalogo.ConsumidorProjecaoFilmes, catalogo.ProjetarFilmeCriado, zap.NewNop())
		if err := client.Consumir(ctx, broker.QueueFilmeCriado, h); err != nil {
			t.Errorf("Consumir: %v", err)
		}
	}()
	defer func() {
		cancel()
		<-relayDone
		<-consumerDone
	}()

	svc := catalogo.NewFilmService(catalogodb.New(pool), pool, nil, zap.NewNop())
	film, err := svc.CreateFilm(context.Background(), catalogo.CreateFilmParams{Title: "Walking skeleton CA05"})
	if err != nil {
		t.Fatalf("CreateFilm: %v", err)
	}

	if !pollUntil(t, 20*time.Second, func() bool { return aplicacoes(t, pool, film.ID) == 1 }) {
		t.Fatalf("filme %d não chegou à projeção (aplicacoes=%d)", film.ID, aplicacoes(t, pool, film.ID))
	}
}
