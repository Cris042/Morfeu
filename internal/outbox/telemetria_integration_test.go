//go:build integration
// +build integration

package outbox_test

import (
	"context"
	"io"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/broker"
	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/telemetria"
)

// Suíte de integração da instrumentação (PRD 0006), no mesmo pacote/TestMain
// dos testes de outbox e consumidor (PG + RabbitMQ reais).

func valorMetrica(t *testing.T, corpo, serie string) (float64, bool) {
	t.Helper()
	re := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(serie) + ` ([0-9.eE+-]+)$`)
	m := re.FindStringSubmatch(corpo)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("valor de %s inválido: %v", serie, err)
	}
	return v, true
}

// TestMetricas_MensageriaComFontesReais cobre CA03: pendente na outbox e
// mensagens na DLQ real aparecem no scrape com os valores verdadeiros.
func TestMetricas_MensageriaComFontesReais(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := broker.NewClient(amqpURL, zap.NewNop())
	if err := client.Start(ctx); err != nil {
		t.Fatalf("conectar ao broker: %v", err)
	}
	defer func() { _ = client.Close() }()

	ch := canalTeste(t)
	if _, err := ch.QueuePurge(broker.QueueFilmeCriadoDLQ, false); err != nil {
		t.Fatalf("purge da DLQ: %v", err)
	}
	const naDLQ = 3
	for range naDLQ {
		publicarNaFila(t, ch, broker.QueueFilmeCriadoDLQ, uuid.NewString(), []byte(`{}`))
	}
	if err := enqueue(context.Background(), pool, "catalogo.filme_criado", uuid.NewString(), []byte(`{"id":1}`)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	tel, err := telemetria.Iniciar(context.Background(), telemetria.Config{Servico: "t", Versao: "t", TaxaAmostragem: 1})
	if err != nil {
		t.Fatalf("Iniciar: %v", err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()
	err = tel.RegistrarMensageria(telemetria.FontesMensageria{
		Pendentes:   func(ctx context.Context) (int64, error) { return outbox.Pendentes(ctx, pool) },
		LagSegundos: func(ctx context.Context) (float64, error) { return outbox.LagSegundos(ctx, pool) },
		ProfundidadeDLQ: func(_ context.Context, fila string) (int, error) {
			return client.ProfundidadeFila(fila)
		},
		FilasDLQ: []string{broker.QueueFilmeCriadoDLQ},
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("RegistrarMensageria: %v", err)
	}

	var corpo string
	ok := pollUntil(t, 10*time.Second, func() bool {
		rec := httptest.NewRecorder()
		tel.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
		b, _ := io.ReadAll(rec.Body)
		corpo = string(b)
		v, ok := valorMetrica(t, corpo, `morfeu_dlq_mensagens{fila="catalogo.filme_criado.dlq"}`)
		return ok && v == naDLQ
	})
	if !ok {
		t.Fatalf("morfeu_dlq_mensagens deveria ser %d:\n%s", naDLQ, corpo)
	}
	if v, ok := valorMetrica(t, corpo, "morfeu_outbox_pendentes"); !ok || v < 1 {
		t.Errorf("morfeu_outbox_pendentes deveria ser >= 1, veio %v (presente=%v)", v, ok)
	}
	if v, ok := valorMetrica(t, corpo, "morfeu_outbox_lag_segundos"); !ok || v <= 0 {
		t.Errorf("morfeu_outbox_lag_segundos deveria ser > 0, veio %v (presente=%v)", v, ok)
	}
}

// TestConsumidor_ContinuaTraceDoProdutor cobre CA05/RF06: o span da entrega
// pertence ao trace do traceparent publicado pelo produtor.
//
// Guard-rail (auditoria 0006): troca o TracerProvider/propagator GLOBAIS e
// restaura no Cleanup — seguro só porque o pacote não usa t.Parallel() nem
// -shuffle. Se isso mudar, injete o provider no broker em vez do global.
func TestConsumidor_ContinuaTraceDoProdutor(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		_ = tp.Shutdown(context.Background())
	})

	ch := canalTeste(t)
	ft := declararFilaTeste(t, ch)

	recebido := make(chan trace.SpanContext, 1)
	iniciarConsumer(t, ft.fila, func(ctx context.Context, _ broker.Entrega) error {
		recebido <- trace.SpanContextFromContext(ctx)
		return nil
	})

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	err := ch.PublishWithContext(context.Background(), "", ft.fila, false, false, amqp.Publishing{
		MessageId: uuid.NewString(),
		Headers:   amqp.Table{"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01"},
		Body:      []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("publicar: %v", err)
	}

	select {
	case sc := <-recebido:
		if sc.TraceID().String() != traceID {
			t.Errorf("span da entrega deveria continuar o trace %s, veio %s", traceID, sc.TraceID())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("entrega não chegou ao handler")
	}

	if !pollUntil(t, 5*time.Second, func() bool { return len(recorder.Ended()) > 0 }) {
		t.Fatal("span de consumo não foi finalizado")
	}
	span := recorder.Ended()[0]
	if span.SpanKind() != trace.SpanKindConsumer || span.Parent().SpanID().String() != "00f067aa0ba902b7" {
		t.Errorf("span esperado consumer filho de 00f067aa0ba902b7; kind=%v pai=%s", span.SpanKind(), span.Parent().SpanID())
	}
}
