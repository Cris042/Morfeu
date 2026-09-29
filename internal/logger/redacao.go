package logger

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// valorRedigido substitui o valor de campos sensíveis (RF07 do PRD 0006).
const valorRedigido = "[REDACTED]"

// fragmentosSensiveis: nome de campo (case-insensitive) que contém qualquer
// um destes tem o valor redigido — senha, credenciais e dado de cartão futuro.
var fragmentosSensiveis = []string{
	"senha", "password", "token", "authorization", "secret", "cartao", "card",
}

// nomesSensiveisExatos: fragmentos curtos demais para busca por substring
// ("pan" casaria com "span_id" e redigiria a própria correlação de trace).
var nomesSensiveisExatos = map[string]bool{"pan": true}

func campoSensivel(chave string) bool {
	k := strings.ToLower(chave)
	if nomesSensiveisExatos[k] {
		return true
	}
	for _, f := range fragmentosSensiveis {
		if strings.Contains(k, f) {
			return true
		}
	}
	return false
}

func redigir(fields []zapcore.Field) []zapcore.Field {
	var out []zapcore.Field
	for i, f := range fields {
		if !campoSensivel(f.Key) {
			continue
		}
		if out == nil {
			out = make([]zapcore.Field, len(fields))
			copy(out, fields)
		}
		out[i] = zap.String(f.Key, valorRedigido)
	}
	if out == nil {
		return fields
	}
	return out
}

// coreRedator embrulha um zapcore.Core e redige campos sensíveis tanto nos
// campos fixos (With) quanto nos de cada entrada (Write). A redação nasce no
// logger (refinamento E0d) para que nenhum ponto de log dependa de disciplina.
type coreRedator struct {
	zapcore.Core
}

// NovoCoreRedator embrulha core com a redação de campos sensíveis.
func NovoCoreRedator(core zapcore.Core) zapcore.Core {
	return coreRedator{Core: core}
}

func (c coreRedator) With(fields []zapcore.Field) zapcore.Core {
	return coreRedator{Core: c.Core.With(redigir(fields))}
}

func (c coreRedator) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}

func (c coreRedator) Write(e zapcore.Entry, fields []zapcore.Field) error {
	return c.Core.Write(e, redigir(fields))
}

// ComTrace devolve l com trace_id/span_id do span ativo em ctx (correlação
// log↔trace, CA04 do PRD 0006). Sem span válido, devolve l inalterado.
// Spans não amostrados também têm IDs — todo log dentro de request/consumo
// carrega o trace_id.
func ComTrace(ctx context.Context, l *zap.Logger) *zap.Logger {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return l
	}
	return l.With(
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	)
}
