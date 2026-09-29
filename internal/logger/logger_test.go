package logger

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewLogger_InfoLevel(t *testing.T) {
	log := NewLogger("info")
	if log == nil {
		t.Fatal("Expected logger, got nil")
	}

	// Should not panic
	log.Info("test message")
}

func TestNewLogger_DebugLevel(t *testing.T) {
	log := NewLogger("debug")
	if log == nil {
		t.Fatal("Expected logger, got nil")
	}

	log.Debug("debug message")
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	log := NewLogger("invalid")
	if log == nil {
		t.Fatal("Expected logger to default to info level")
	}
}

func TestLogger_StringField(t *testing.T) {
	log := NewLogger("info")
	field := log.String("key", "value")

	// Check that field is of correct type
	if field.Type != zapcore.StringType {
		t.Errorf("Expected String field type, got %v", field.Type)
	}
}

func TestLogger_IntField(t *testing.T) {
	log := NewLogger("info")
	field := log.Int("key", 42)

	if field.Type != zapcore.Int64Type {
		t.Errorf("Expected Int field type, got %v", field.Type)
	}
}

func TestLogger_BoolField(t *testing.T) {
	log := NewLogger("info")
	field := log.Bool("key", true)

	if field.Type != zapcore.BoolType {
		t.Errorf("Expected Bool field type, got %v", field.Type)
	}
}

// logObservado monta um logger com o core de redação sobre um observer.
func logObservado() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return zap.New(NovoCoreRedator(core)), logs
}

// TestRedacao_CamposSensiveis cobre CA06 do PRD 0006.
func TestRedacao_CamposSensiveis(t *testing.T) {
	l, logs := logObservado()
	l.With(zap.String("api_token", "t0k")).Info("evento",
		zap.String("Authorization", "Bearer abc"),
		zap.String("senha", "s3gr3d0"),
		zap.String("pan", "4111111111111111"),
		zap.String("event_type", "catalogo.filme_criado"),
		zap.String("span_id", "00f067aa0ba902b7"),
	)

	ctx := logs.All()[0].ContextMap()
	for _, k := range []string{"api_token", "Authorization", "senha", "pan"} {
		if ctx[k] != valorRedigido {
			t.Errorf("campo %s deveria ser %s, veio %v", k, valorRedigido, ctx[k])
		}
	}
	if ctx["event_type"] != "catalogo.filme_criado" || ctx["span_id"] != "00f067aa0ba902b7" {
		t.Errorf("campos comuns não podem ser redigidos: %v", ctx)
	}
}

// TestComTrace_CorrelacionaComSpan cobre CA04 do PRD 0006.
func TestComTrace_CorrelacionaComSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	ctx, span := tp.Tracer("teste").Start(context.Background(), "op")
	defer span.End()

	l, logs := logObservado()
	ComTrace(ctx, l).Info("dentro do span")

	got := logs.All()[0].ContextMap()
	if got["trace_id"] != span.SpanContext().TraceID().String() {
		t.Errorf("trace_id %v != span %s", got["trace_id"], span.SpanContext().TraceID())
	}
	if got["span_id"] != span.SpanContext().SpanID().String() {
		t.Errorf("span_id %v != span %s", got["span_id"], span.SpanContext().SpanID())
	}

	l2, logs2 := logObservado()
	ComTrace(context.Background(), l2).Info("sem span")
	if _, ok := logs2.All()[0].ContextMap()["trace_id"]; ok {
		t.Error("sem span válido não deve haver trace_id")
	}
}
