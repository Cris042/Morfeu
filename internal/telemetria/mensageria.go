package telemetria

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// FontesMensageria são as leituras que alimentam os gauges de mensageria
// (RF04 do PRD 0006). Funções em vez de tipos concretos: telemetria não
// importa outbox nem broker; o wiring (main) injeta as fontes. Campos nil
// desligam o respectivo gauge (ex.: processo sem broker não mede DLQ).
type FontesMensageria struct {
	Pendentes   func(ctx context.Context) (int64, error)
	LagSegundos func(ctx context.Context) (float64, error)
	// ProfundidadeDLQ lê a profundidade de cada fila de FilasDLQ (PRD 0027:
	// todas as DLQs, uma série por fila).
	ProfundidadeDLQ func(ctx context.Context, fila string) (int, error)
	FilasDLQ        []string
}

// RegistrarMensageria cria os gauges observáveis morfeu_outbox_pendentes,
// morfeu_outbox_lag_segundos e morfeu_dlq_mensagens{fila}. São lidos só no
// scrape (callback), sem goroutine própria. Falha de leitura omite a
// observação e loga warn — o scrape nunca falha por causa de uma fonte.
func (t *Telemetria) RegistrarMensageria(fontes FontesMensageria, logger *zap.Logger) error {
	meter := t.Meter("morfeu/mensageria")

	pendentes, err := meter.Int64ObservableGauge("morfeu_outbox_pendentes",
		metric.WithDescription("Eventos da outbox ainda não publicados no broker."))
	if err != nil {
		return fmt.Errorf("telemetria: gauge pendentes: %w", err)
	}
	lag, err := meter.Float64ObservableGauge("morfeu_outbox_lag_segundos",
		metric.WithDescription("Idade do evento pendente mais antigo da outbox (0 se nenhum)."))
	if err != nil {
		return fmt.Errorf("telemetria: gauge lag: %w", err)
	}
	dlq, err := meter.Int64ObservableGauge("morfeu_dlq_mensagens",
		metric.WithDescription("Mensagens paradas na DLQ."))
	if err != nil {
		return fmt.Errorf("telemetria: gauge dlq: %w", err)
	}
	g := gaugesMensageria{fontes: fontes, logger: logger, pendentes: pendentes, lag: lag, dlq: dlq}
	_, err = meter.RegisterCallback(g.observar, pendentes, lag, dlq)
	if err != nil {
		return fmt.Errorf("telemetria: callback de mensageria: %w", err)
	}
	return nil
}

// gaugesMensageria agrupa os gauges e as fontes para o callback do scrape.
type gaugesMensageria struct {
	fontes    FontesMensageria
	logger    *zap.Logger
	pendentes metric.Int64ObservableGauge
	lag       metric.Float64ObservableGauge
	dlq       metric.Int64ObservableGauge
}

// observar lê cada fonte configurada; erro de leitura omite só a respectiva
// observação (o scrape nunca falha por causa de uma fonte).
func (g gaugesMensageria) observar(ctx context.Context, o metric.Observer) error {
	if g.fontes.Pendentes != nil {
		n, err := g.fontes.Pendentes(ctx)
		g.registrar(err, "outbox_pendentes", func() { o.ObserveInt64(g.pendentes, n) })
	}
	if g.fontes.LagSegundos != nil {
		s, err := g.fontes.LagSegundos(ctx)
		g.registrar(err, "outbox_lag", func() { o.ObserveFloat64(g.lag, s) })
	}
	if g.fontes.ProfundidadeDLQ != nil {
		for _, fila := range g.fontes.FilasDLQ {
			n, err := g.fontes.ProfundidadeDLQ(ctx, fila)
			g.registrar(err, "dlq_mensagens", func() {
				o.ObserveInt64(g.dlq, int64(n), metric.WithAttributes(attribute.String("fila", fila)))
			})
		}
	}
	return nil
}

func (g gaugesMensageria) registrar(err error, nome string, observar func()) {
	if err != nil {
		g.logger.Warn("métrica indisponível no scrape", zap.String("metrica", nome), zap.Error(err))
		return
	}
	observar()
}
