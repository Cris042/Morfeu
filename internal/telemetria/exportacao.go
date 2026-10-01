package telemetria

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Exportação de traces (ADR 0012, PRD 0040): com coletor configurado o app
// exporta 100% por OTLP/HTTP e o Alloy decide por tail sampling; sem coletor,
// amostragem de cabeça com descarte (comportamento até o E9). A exportação
// nunca bloqueia a requisição: lote com fila limitada (descarta o excesso) e
// timeout curto — o app sobe e atende com o coletor fora do ar.

const (
	filaMaxima         = 2048
	loteMaximo         = 512
	intervaloDoLote    = 2 * time.Second
	timeoutExportar    = 3 * time.Second
	timeoutConexaoHTTP = 2 * time.Second
)

// processadorOTLP monta o exporter OTLP/HTTP sanitizado em lote.
func processadorOTLP(ctx context.Context, endpoint string) (sdktrace.SpanProcessor, error) {
	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithTimeout(timeoutConexaoHTTP),
		// Sem retry: perder um lote é aceitável; acumular não é.
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetria: exporter OTLP: %w", err)
	}
	return sdktrace.NewBatchSpanProcessor(Sanitizar(exp),
		sdktrace.WithMaxQueueSize(filaMaxima),
		sdktrace.WithMaxExportBatchSize(loteMaximo),
		sdktrace.WithBatchTimeout(intervaloDoLote),
		sdktrace.WithExportTimeout(timeoutExportar),
	), nil
}

// Sanitizar embrulha o exporter: nenhum atributo que possa carregar
// credencial ou PII sai do processo (refinamento E10, security). O caminho
// vira a rota templada; query, URL completa, headers e parâmetros de query de
// banco são descartados. É a defesa primária — o Alloy repete a remoção.
func Sanitizar(exp sdktrace.SpanExporter) sdktrace.SpanExporter {
	return exporterSanitizado{exp}
}

type exporterSanitizado struct{ sdktrace.SpanExporter }

func (e exporterSanitizado) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	limpos := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		limpos[i] = spanSanitizado{ReadOnlySpan: s, attrs: atributosLimpos(s.Attributes())}
	}
	return e.SpanExporter.ExportSpans(ctx, limpos)
}

type spanSanitizado struct {
	sdktrace.ReadOnlySpan
	attrs []attribute.KeyValue
}

func (s spanSanitizado) Attributes() []attribute.KeyValue { return s.attrs }

// prefixosProibidos: atributos que nunca são exportados.
var prefixosProibidos = []string{
	"url.query", "url.full", "http.url", "http.target",
	"http.request.header.", "http.response.header.",
	"db.query.parameter.", "enduser.", "user.",
}

func atributosLimpos(attrs []attribute.KeyValue) []attribute.KeyValue {
	var rota string
	for _, a := range attrs {
		if a.Key == "http.route" {
			rota = a.Value.AsString()
		}
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		k := string(a.Key)
		if proibido(k) {
			continue
		}
		// O path real pode carregar o token do ingresso (/i/{id}.{token}):
		// com rota conhecida, exporta só o template.
		if k == "url.path" && rota != "" {
			a = attribute.String("url.path", rota)
		}
		out = append(out, a)
	}
	return out
}

func proibido(chave string) bool {
	for _, p := range prefixosProibidos {
		if chave == strings.TrimSuffix(p, ".") || strings.HasPrefix(chave, p) {
			return true
		}
	}
	return false
}
