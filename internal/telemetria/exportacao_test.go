package telemetria

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Exportação OTLP (PRD 0040, ADR 0012): coletor falso em httptest — sem rede.

type coletorFalso struct {
	mu     sync.Mutex
	corpos [][]byte
}

func (c *coletorFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.corpos = append(c.corpos, b)
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *coletorFalso) tudo() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Join(c.corpos, nil)
}

func iniciarComColetor(t *testing.T, endpoint string) *Telemetria {
	t.Helper()
	tel, err := Iniciar(context.Background(), Config{Servico: "morfeu-teste", Versao: "t", TaxaAmostragem: TaxaAmostragemPadrao, EndpointOTLP: endpoint})
	if err != nil {
		t.Fatalf("Iniciar: %v", err)
	}
	return tel
}

// TestOTLP_ExportaSanitizado: com coletor, todo trace é exportado (o Alloy
// decide) e nada que possa ser credencial ou PII sai do processo — o path do
// ingresso vira a rota, query/headers/URL completa somem.
func TestOTLP_ExportaSanitizado(t *testing.T) {
	coletor := &coletorFalso{}
	srv := httptest.NewServer(coletor)
	defer srv.Close()
	tel := iniciarComColetor(t, srv.URL)

	if got, quer := tel.sampler.Description(), sdktrace.ParentBased(sdktrace.AlwaysSample()).Description(); got != quer {
		t.Fatalf("com coletor o app exporta 100%%: sampler %q, quer %q", got, quer)
	}
	_, span := tel.TracerProvider.Tracer("teste").Start(context.Background(), "GET /i/:ref")
	span.SetAttributes(
		attribute.String("http.route", "/i/:ref"),
		attribute.String("url.path", "/i/0b9e6c62-51a5-4bd6-9a55-2f0a8b3c4d5e.TOKENSECRETOdoINGRESSO"),
		attribute.String("url.query", "email=ana@exemplo.com"),
		attribute.String("url.full", "https://morfeu.exemplo/i/x.TOKENSECRETOdoINGRESSO"),
		attribute.String("http.request.header.authorization", "Bearer segredo-jwt"),
		attribute.String("db.query.parameter.0", "ana@exemplo.com"),
		attribute.String("pedido_id", "6f1c2e0a-0000-4000-8000-000000000001"),
	)
	span.SetStatus(codes.Error, "falhou")
	span.End()
	if err := tel.TracerProvider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	corpo := coletor.tudo()
	if len(corpo) == 0 {
		t.Fatal("o coletor não recebeu nada")
	}
	for _, deve := range []string{"/i/:ref", "pedido_id", "morfeu-teste"} {
		if !bytes.Contains(corpo, []byte(deve)) {
			t.Errorf("faltou %q no OTLP exportado", deve)
		}
	}
	for _, nunca := range []string{"TOKENSECRETOdoINGRESSO", "ana@exemplo.com", "segredo-jwt", "url.query", "url.full"} {
		if bytes.Contains(corpo, []byte(nunca)) {
			t.Errorf("vazou %q no OTLP exportado", nunca)
		}
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// TestOTLP_ColetorForaNaoBloqueia: coletor inacessível — criar spans não
// trava a requisição (fila limitada descarta) e o desligamento termina no prazo.
func TestOTLP_ColetorForaNaoBloqueia(t *testing.T) {
	tel := iniciarComColetor(t, "http://127.0.0.1:1")
	tracer := tel.TracerProvider.Tracer("teste")
	inicio := time.Now()
	for range 5 * filaMaxima {
		_, s := tracer.Start(context.Background(), "requisicao")
		s.End()
	}
	if d := time.Since(inicio); d > time.Second {
		t.Fatalf("criar spans com o coletor fora levou %s — a exportação bloqueou a requisição", d)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inicio = time.Now()
	err := tel.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) || time.Since(inicio) > 10*time.Second {
		t.Fatalf("shutdown sem coletor passou do prazo: %v", err)
	}
}

// TestSanitizar_SemRotaMantemPath: span sem rota (ex.: broker) mantém o
// path; prefixos proibidos caem sempre.
func TestSanitizar_SemRotaMantemPath(t *testing.T) {
	got := atributosLimpos([]attribute.KeyValue{
		attribute.String("url.path", "/fila"),
		attribute.String("http.response.header.set-cookie", "x"),
		attribute.String("user.email", "ana@exemplo.com"),
		attribute.Int("messaging.batch", 1),
	})
	if len(got) != 2 || got[0].Value.AsString() != "/fila" || got[1].Key != "messaging.batch" {
		t.Fatalf("atributos: %v", got)
	}
}
