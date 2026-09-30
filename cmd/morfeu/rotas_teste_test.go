package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/mclovin137/morfeu/internal/config"
	"github.com/mclovin137/morfeu/internal/notificacao"
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

// TestRotasDeTesteAtivas: só gateway fake com o segredo do webhook.
func TestRotasDeTesteAtivas(t *testing.T) {
	casos := []struct {
		gw, seg string
		quer    bool
	}{{"fake", "whsec_x", true}, {"fake", "", false}, {"stripe", "whsec_x", false}}
	for _, c := range casos {
		if got := rotasDeTesteAtivas(&config.Config{Gateway: c.gw, StripeWebhookSegredo: c.seg}); got != c.quer {
			t.Errorf("%+v: %t", c, got)
		}
	}
}
