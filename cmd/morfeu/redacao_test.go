package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestRedacaoDoIngresso cobre o PRD 0034: o token do link do ingresso nunca
// chega ao access log nem ao span (url.path) — nem em 404 de rota.
func TestRedacaoDoIngresso(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	e := echo.New()
	e.Use(otelecho.Middleware("teste", otelecho.WithTracerProvider(tp)))
	e.Use(redigirSpanDoIngresso)
	var logado []string
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			logado = append(logado, uriParaLog(c, c.Request().RequestURI))
			return err
		}
	})
	ok := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
	e.GET("/i/:ref", ok)
	e.GET("/i/:ref/qr.png", ok)
	e.GET("/filmes", ok)

	const segredo = "abc.TOKENSECRETO"
	for _, p := range []string{"/i/" + segredo, "/i/" + segredo + "/qr.png", "/i/" + segredo + "/x/y", "/filmes?pagina=2"} {
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	want := []string{"/i/:ref", "/i/:ref/qr.png", "/i/[redigido]", "/filmes?pagina=2"}
	if strings.Join(logado, " ") != strings.Join(want, " ") {
		t.Fatalf("log: %v, quero %v", logado, want)
	}
	for _, s := range rec.Ended() {
		if strings.Contains(s.Name(), "TOKENSECRETO") {
			t.Errorf("span com o token no nome: %s", s.Name())
		}
		for _, a := range s.Attributes() {
			if strings.Contains(a.Value.String(), "TOKENSECRETO") {
				t.Errorf("span %s com o token em %s", s.Name(), a.Key)
			}
		}
	}
}
