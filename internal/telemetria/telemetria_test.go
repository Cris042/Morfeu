package telemetria

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

func iniciarTeste(t *testing.T) *Telemetria {
	t.Helper()
	tel, err := Iniciar(context.Background(), Config{Servico: "morfeu-teste", Versao: "t", TaxaAmostragem: TaxaAmostragemPadrao})
	if err != nil {
		t.Fatalf("Iniciar: %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })
	return tel
}

// servidorTeste monta um Echo com o middleware real, uma rota /filmes e o
// /metrics, como no main.
func servidorTeste(tel *Telemetria) *echo.Echo {
	e := echo.New()
	e.Use(tel.MiddlewareHTTP("morfeu-teste"))
	e.GET("/filmes", func(c echo.Context) error { return c.JSON(http.StatusOK, []string{}) })
	e.GET("/health", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/metrics", echo.WrapHandler(tel.Handler()))
	return e
}

func get(t *testing.T, e *echo.Echo, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	b, _ := io.ReadAll(rec.Body)
	return string(b)
}

// TestGoldenSignals_RotaRoteada cobre CA01: requests contam por rota roteada;
// 404 com path aleatório não vira série com o path.
func TestGoldenSignals_RotaRoteada(t *testing.T) {
	tel := iniciarTeste(t)
	e := servidorTeste(tel)

	get(t, e, "/filmes")
	get(t, e, "/filmes")
	aleatorio := "/nao-existe-" + uuid.NewString()
	get(t, e, aleatorio)
	get(t, e, "/health")

	metrics := get(t, e, "/metrics")
	if !strings.Contains(metrics, `http_server_request_duration_seconds_count{`) {
		t.Fatalf("histograma de golden signals ausente:\n%s", metrics)
	}
	var linhaFilmes string
	for _, l := range strings.Split(metrics, "\n") {
		if strings.HasPrefix(l, "http_server_request_duration_seconds_count{") && strings.Contains(l, `http_route="/filmes"`) {
			linhaFilmes = l
		}
	}
	if !strings.HasSuffix(linhaFilmes, " 2") {
		t.Errorf("esperava contagem 2 para /filmes, linha: %q", linhaFilmes)
	}
	if strings.Contains(metrics, aleatorio) || strings.Contains(metrics, `http_route="/health"`) {
		t.Errorf("path bruto ou /health vazaram para as métricas:\n%s", metrics)
	}
}

// labelsForaDaAllowlist coleta o registry e devolve os labels não permitidos.
func labelsForaDaAllowlist(t *testing.T, tel *Telemetria) []string {
	t.Helper()
	permitidos := map[string]bool{}
	for _, k := range LabelsPermitidos {
		// Prometheus troca "." por "_" nos nomes de label.
		permitidos[strings.ReplaceAll(string(k), ".", "_")] = true
	}
	familias, err := tel.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var fora []string
	for _, f := range familias {
		for _, m := range f.GetMetric() {
			for _, lp := range m.GetLabel() {
				if !permitidos[lp.GetName()] {
					fora = append(fora, f.GetName()+"{"+lp.GetName()+"}")
				}
			}
		}
	}
	return fora
}

// TestCardinalidade_SoLabelsPermitidos cobre CA02/RNF01: depois de tráfego
// real e das métricas de mensageria, todo label ∈ allowlist. A prova de que o
// teste detecta violação: um instrumento com user_id e path bruto tem esses
// atributos DESCARTADOS pela view — nunca chegam ao registry.
func TestCardinalidade_SoLabelsPermitidos(t *testing.T) {
	tel := iniciarTeste(t)
	e := servidorTeste(tel)
	get(t, e, "/filmes")
	get(t, e, "/x/"+uuid.NewString())

	err := tel.RegistrarMensageria(FontesMensageria{
		Pendentes:       func(context.Context) (int64, error) { return 3, nil },
		LagSegundos:     func(context.Context) (float64, error) { return 1.5, nil },
		ProfundidadeDLQ: func(context.Context) (int, error) { return 2, nil },
		FilaDLQ:         "catalogo.filme_criado.dlq",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("RegistrarMensageria: %v", err)
	}

	violador, err := tel.Meter("teste").Int64Counter("teste_violador")
	if err != nil {
		t.Fatalf("counter: %v", err)
	}
	violador.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("user_id", uuid.NewString()),
		attribute.String("url.path", "/assento/"+uuid.NewString()),
	))

	if fora := labelsForaDaAllowlist(t, tel); len(fora) > 0 {
		t.Errorf("labels fora da allowlist chegaram ao registry: %v", fora)
	}
	if strings.Contains(get(t, e, "/metrics"), "user_id") {
		t.Error("atributo user_id vazou para /metrics")
	}
}

// TestLabelsDaSaga cobre a auditoria 0026: as labels fechadas do checkout
// chegam ao /metrics — os alertas da saga dependem delas.
func TestLabelsDaSaga(t *testing.T) {
	tel := iniciarTeste(t)
	e := servidorTeste(tel)
	m := tel.Meter("teste-saga")
	ctx := context.Background()
	compensacoes, _ := m.Int64Counter("saga_compensacoes_total")
	compensacoes.Add(ctx, 1, metric.WithAttributes(attribute.String("passo", "estorno")))
	funil, _ := m.Int64Counter("checkout_funil_total")
	funil.Add(ctx, 1, metric.WithAttributes(attribute.String("etapa", "pago")))
	gw, _ := m.Int64Counter("gateway_requests_total")
	gw.Add(ctx, 1, metric.WithAttributes(attribute.String("op", "estornar"), attribute.String("resultado", "ok")))
	presos, _ := m.Int64ObservableGauge("pedidos_presos")
	_, _ = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(presos, 2, metric.WithAttributes(attribute.String("estado", "estorno_pendente")))
		o.ObserveInt64(presos, 0, metric.WithAttributes(attribute.String("estado", "aguardando_vencido")))
		return nil
	}, presos)
	corpo := get(t, e, "/metrics")
	for _, serie := range []string{
		`saga_compensacoes_total{passo="estorno"} 1`,
		`etapa="pago"`, `op="estornar"`,
		`pedidos_presos{estado="estorno_pendente"`, `pedidos_presos{estado="aguardando_vencido"`,
	} {
		if !strings.Contains(corpo, serie) {
			t.Errorf("série ausente no /metrics: %s", serie)
		}
	}
}

// TestMensageria_GaugesNoScrape cobre RF04: valores das fontes aparecem no
// scrape; fonte com erro omite só a própria observação.
func TestMensageria_GaugesNoScrape(t *testing.T) {
	tel := iniciarTeste(t)
	err := tel.RegistrarMensageria(FontesMensageria{
		Pendentes:       func(context.Context) (int64, error) { return 7, nil },
		LagSegundos:     func(context.Context) (float64, error) { return 0, io.ErrUnexpectedEOF },
		ProfundidadeDLQ: func(context.Context) (int, error) { return 4, nil },
		FilaDLQ:         "catalogo.filme_criado.dlq",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("RegistrarMensageria: %v", err)
	}
	metrics := get(t, servidorTeste(tel), "/metrics")

	for _, esperado := range []string{
		"morfeu_outbox_pendentes 7",
		`morfeu_dlq_mensagens{fila="catalogo.filme_criado.dlq"} 4`,
	} {
		if !strings.Contains(metrics, esperado) {
			t.Errorf("esperava %q no scrape:\n%s", esperado, metrics)
		}
	}
	if strings.Contains(metrics, "morfeu_outbox_lag_segundos ") {
		t.Error("fonte com erro não deveria produzir observação")
	}
}

// TestIniciar_SamplerEExporter cobre CA07: sampler ParentBased(10%) e exporter
// de descarte explícito.
func TestIniciar_SamplerEExporter(t *testing.T) {
	tel := iniciarTeste(t)

	desc := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(TaxaAmostragemPadrao)).Description()
	if got := tel.sampler.Description(); got != desc {
		t.Errorf("sampler esperado %q, recebi %q", desc, got)
	}

	var exp sdktrace.SpanExporter = ExporterDescarte{}
	if err := exp.ExportSpans(context.Background(), nil); err != nil {
		t.Errorf("exporter de descarte não pode falhar: %v", err)
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
