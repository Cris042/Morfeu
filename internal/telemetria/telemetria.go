// Package telemetria é a plataforma de observabilidade do binário (PRD 0006,
// refinamento E0 §"Task E0d"): TracerProvider com sampling e exporter de
// descarte explícito (sem Tempo até o E6/E10), MeterProvider exportando no
// formato Prometheus num registry dedicado e o handler de /metrics. Domínios
// nunca importam este pacote nem OTel (ADR 0003) — a instrumentação entra por
// middleware (Echo), tracer do pgx e pelo broker.
package telemetria

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// TaxaAmostragemPadrao: 10% dos traces raiz (refinamento E0d). Traces com pai
// seguem a decisão do pai (ParentBased), preservando traces distribuídos.
const TaxaAmostragemPadrao = 0.1

// LabelsPermitidos é a allowlist fechada de atributos de métricas (RNF01):
// qualquer outro atributo é descartado pela view do SDK antes de virar label
// — inclusive server.address/url.* que o middleware HTTP derivaria de headers
// controlados pelo cliente. Proibido: ids de assento/sessão/usuário e path bruto.
var LabelsPermitidos = []attribute.Key{
	"http.route",
	"http.request.method",
	"http.response.status_code",
	"fila",
	// auth (task 0008): valores fechados — resultado do login e escopo do limitador.
	"resultado",
	"escopo",
	// TMDB (task 0012): valores fechados — operação e classe de status.
	"operacao",
	"classe_status",
	// Checkout (tasks 0023–0026, auditoria 0026): valores fechados — etapa do
	// funil, operação do gateway, passo da compensação e estado dos presos.
	// Sem eles os alertas da saga liam séries sem label (nunca disparavam).
	"etapa",
	"op",
	"passo",
	"estado",
}

// Config parametriza Iniciar.
type Config struct {
	Servico        string
	Versao         string
	TaxaAmostragem float64
}

// Telemetria agrupa os providers e o registry de /metrics.
type Telemetria struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	registry       *prometheus.Registry
	sampler        sdktrace.Sampler
}

// Iniciar configura traces e métricas e os registra como globais (junto com
// o propagator W3C TraceContext usado no envelope AMQP — ADR 0007).
func Iniciar(ctx context.Context, cfg Config) (*Telemetria, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(cfg.Servico),
		semconv.ServiceVersion(cfg.Versao),
	))
	if err != nil {
		return nil, fmt.Errorf("telemetria: resource: %w", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TaxaAmostragem))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(ExporterDescarte{})),
	)

	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(registry),
		otelprom.WithoutScopeInfo(),
		otelprom.WithoutTargetInfo(),
	)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, fmt.Errorf("telemetria: exporter prometheus: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exporter),
		sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: "*"},
			sdkmetric.Stream{AttributeFilter: attribute.NewAllowKeysFilter(LabelsPermitidos...)},
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return &Telemetria{TracerProvider: tp, MeterProvider: mp, registry: registry, sampler: sampler}, nil
}

// Handler serve o registry dedicado no formato de exposição do Prometheus.
func (t *Telemetria) Handler() http.Handler {
	return promhttp.HandlerFor(t.registry, promhttp.HandlerOpts{})
}

// Registry expõe o registry para inspeção em testes (CA02).
func (t *Telemetria) Registry() *prometheus.Registry {
	return t.registry
}

// Meter devolve um meter do provider desta telemetria.
func (t *Telemetria) Meter(nome string) metric.Meter {
	return t.MeterProvider.Meter(nome)
}

// Shutdown encerra os providers; chamar no fim do processo.
func (t *Telemetria) Shutdown(ctx context.Context) error {
	return errors.Join(t.TracerProvider.Shutdown(ctx), t.MeterProvider.Shutdown(ctx))
}

// ExporterDescarte é o exporter de traces explícito desta fase: spans
// amostrados são descartados (sem Tempo/OTLP até o E6/E10). Existe para que o
// pipeline de traces esteja completo — trocar por OTLP é mudar uma linha.
type ExporterDescarte struct{}

// ExportSpans descarta os spans.
func (ExporterDescarte) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }

// Shutdown não tem recurso a liberar.
func (ExporterDescarte) Shutdown(context.Context) error { return nil }

// Tracer é um atalho para o tracer global com o nome do componente.
func Tracer(nome string) trace.Tracer {
	return otel.Tracer(nome)
}

// MiddlewareHTTP instrumenta o Echo (RF03 do PRD 0006): span por request e o
// histograma http.server.request.duration com http.route (rota roteada, nunca
// o path bruto). /metrics e /health ficam de fora — scrape e probes não são
// tráfego de usuário e só poluiriam os golden signals.
func (t *Telemetria) MiddlewareHTTP(servico string) echo.MiddlewareFunc {
	return otelecho.Middleware(servico,
		otelecho.WithTracerProvider(t.TracerProvider),
		otelecho.WithMeterProvider(t.MeterProvider),
		otelecho.WithSkipper(func(c echo.Context) bool {
			p := c.Request().URL.Path
			return p == "/metrics" || p == "/health"
		}),
	)
}
