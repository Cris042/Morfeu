package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/config"
	"github.com/mclovin137/morfeu/internal/logger"
	"github.com/mclovin137/morfeu/internal/notificacao"
	"github.com/mclovin137/morfeu/internal/telemetria"
)

// TestEmailsParaTeste cobre o PRD 0035: o E2E lê do fake os links do e-mail
// do destinatário (e só dele).
func TestEmailsParaTeste(t *testing.T) {
	fake := notificacao.NovoFake()
	link := "/i/0b9e6c62-51a5-4bd6-9a55-2f0a8b3c4d5e.abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	_ = fake.Enviar(context.Background(), notificacao.Mensagem{Tipo: "confirmacao", Para: "Ana@Exemplo.com", Assunto: "Seus ingressos — Morfeu",
		Texto: "A1: https://morfeu.exemplo" + link + "\n"})
	_ = fake.Enviar(context.Background(), notificacao.Mensagem{Tipo: "confirmacao", Para: "bia@exemplo.com", Texto: "outro"})

	e := echo.New()
	e.GET("/__teste/emails", emailsParaTeste(fake))
	ler := func(q string) []struct {
		Assunto string   `json:"assunto"`
		Links   []string `json:"links"`
	} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__teste/emails"+q, nil))
		var out []struct {
			Assunto string   `json:"assunto"`
			Links   []string `json:"links"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("%s: %d %s", q, rec.Code, rec.Body)
		}
		return out
	}
	if got := ler("?para=ana@exemplo.com"); len(got) != 1 || len(got[0].Links) != 1 || got[0].Links[0] != link {
		t.Fatalf("ana: %+v", got)
	}
	if got := ler("?para=ninguem@exemplo.com"); len(got) != 0 {
		t.Fatalf("outro destinatário: %+v", got)
	}
	if got := ler(""); len(got) != 0 {
		t.Fatalf("sem destinatário lista tudo: %+v", got)
	}
}

// TestRotasDeTesteAtivas: só gateway fake com o segredo do webhook e fora de
// produção (PRD 0045 RF04/CA03) — em produção a rota some (404).
func TestRotasDeTesteAtivas(t *testing.T) {
	casos := []struct {
		amb, gw, seg string
		quer         bool
	}{
		{"dev", "fake", "whsec_x", true},
		{"dev", "fake", "", false},
		{"dev", "stripe", "whsec_x", false},
		{"producao", "fake", "whsec_x", false},
		{"producao", "stripe", "whsec_x", false},
	}
	for _, c := range casos {
		cfg := &config.Config{Ambiente: c.amb, Gateway: c.gw, StripeWebhookSegredo: c.seg}
		if got := rotasDeTesteAtivas(cfg); got != c.quer {
			t.Errorf("%+v: %t", c, got)
		}
		// Mesmo predicado que o main usa para registrar a rota: 404 quando inativo.
		e := echo.New()
		if rotasDeTesteAtivas(cfg) {
			e.POST("/__teste/pagar/:id", func(ctx echo.Context) error { return ctx.NoContent(http.StatusNoContent) })
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/__teste/pagar/1", nil))
		if want := map[bool]int{true: http.StatusNoContent, false: http.StatusNotFound}[c.quer]; rec.Code != want {
			t.Errorf("%+v: status %d, quer %d", c, rec.Code, want)
		}
	}
}

func TestLimiteEfetivo(t *testing.T) {
	casos := []struct {
		base     int
		loadtest bool
		quer     int
	}{
		{30, false, 30},
		{30, true, 30000},
		{10, true, 10000},
		{300, false, 300},
	}
	for _, c := range casos {
		if got := limiteEfetivo(c.base, c.loadtest); got != c.quer {
			t.Errorf("limiteEfetivo(%d, %t) = %d, quer %d", c.base, c.loadtest, got, c.quer)
		}
	}
}

// TestLimitesDasTravas_DonoSegueBarrando (PRD 0045 CA02): com a flag o limite
// por IP sobe e o por dono continua em 20/min e barra. Redis inalcançável =
// contador em memória do limitador (mesma lógica de contagem).
func TestLimitesDasTravas_DonoSegueBarrando(t *testing.T) {
	ipSem, donoSem := limitesDasTravas(false)
	ipCom, donoCom := limitesDasTravas(true)
	if ipSem != limiteTravasIP || donoSem != limiteTravasDono {
		t.Fatalf("sem flag: ip=%d dono=%d", ipSem, donoSem)
	}
	if ipCom != limiteTravasIP*multiplicadorLoadTest || donoCom != limiteTravasDono {
		t.Fatalf("com flag: ip=%d dono=%d", ipCom, donoCom)
	}

	cli := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = cli.Close() })
	novo := func(prefixo string, max int) *autenticacao.Limitador {
		l, err := autenticacao.NovoLimitador(autenticacao.ConfigLimitador{Redis: cli, Prefixo: prefixo, Max: max, Janela: time.Minute}, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	ctx := context.Background()
	dono, ip := novo("t:dono:", donoCom), novo("t:ip:", ipCom)
	for i := 0; i < limiteTravasDono; i++ {
		dono.RegistrarFalha(ctx, "d1")
		ip.RegistrarFalha(ctx, "1.2.3.4")
	}
	if !dono.Bloqueado(ctx, "d1") {
		t.Error("o limite por dono deveria barrar em 20 mesmo com a flag")
	}
	if ip.Bloqueado(ctx, "1.2.3.4") {
		t.Error("o limite por IP deveria estar folgado com a flag")
	}
}

// TestModoLoadTest_Gauge: morfeu_modo_loadtest vale 1 com a flag e 0 sem ela.
func TestModoLoadTest_Gauge(t *testing.T) {
	for _, ativo := range []bool{false, true} {
		tel, err := telemetria.Iniciar(context.Background(), telemetria.Config{Servico: "t", Versao: "0", TaxaAmostragem: 0})
		if err != nil {
			t.Fatal(err)
		}
		registrarModoLoadTest(tel.Meter("t"), &config.Config{LoadTest: ativo}, logger.NewLogger("error"))
		rec := httptest.NewRecorder()
		tel.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		want := "morfeu_modo_loadtest 0"
		if ativo {
			want = "morfeu_modo_loadtest 1"
		}
		if !strings.Contains(rec.Body.String(), want+"\n") {
			t.Errorf("ativo=%t: série ausente (%q)", ativo, want)
		}
		_ = tel.Shutdown(context.Background())
	}
}
