package notificacao

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
)

// Adapter Resend contra um servidor HTTP falso (PRD 0029) — sem rede.

type resendFalso struct {
	status int
	nome   string
	lento  time.Duration
	req    *http.Request
	corpo  emailResend
}

func (f *resendFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.req = r
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &f.corpo)
	if f.lento > 0 {
		time.Sleep(f.lento)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.status)
	if f.status >= 300 {
		_, _ = io.WriteString(w, `{"statusCode":`+strconv.Itoa(f.status)+`,"name":"`+f.nome+`","message":"eco de ana@exemplo.com"}`)
		return
	}
	_, _ = io.WriteString(w, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)
}

func resendTeste(t *testing.T, f *resendFalso) *Resend {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	r, err := NovoResend(ConfigResend{Chave: "re_teste_chave", Remetente: "Morfeu <ingressos@morfeu.exemplo>", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mensagemTeste(t *testing.T) Mensagem {
	t.Helper()
	m, err := entregadorTeste(t, fonteFixa{}, NovoFake()).Montar(dadosTeste())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestResend_Envio cobre CA01: POST /emails com Authorization, chave de
// idempotência, remetente, texto, HTML e os QRs inline (content_id + base64).
func TestResend_Envio(t *testing.T) {
	f := &resendFalso{status: http.StatusOK}
	m := mensagemTeste(t)
	if err := resendTeste(t, f).Enviar(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	h := f.req.Header
	if f.req.Method != http.MethodPost || f.req.URL.Path != "/emails" || h.Get("Authorization") != "Bearer re_teste_chave" ||
		h.Get("Idempotency-Key") != "confirmacao-11111111-2222-3333-4444-555555555555" || h.Get("Content-Type") != "application/json" {
		t.Fatalf("requisição: %s %s %v", f.req.Method, f.req.URL.Path, h)
	}
	c := f.corpo
	if c.From != "Morfeu <ingressos@morfeu.exemplo>" || len(c.To) != 1 || c.To[0] != "ana@exemplo.com" ||
		c.Subject != m.Assunto || c.HTML != m.HTML || c.Text != m.Texto || len(c.Attachments) != 2 {
		t.Fatalf("corpo: %+v", c)
	}
	a := c.Attachments[0]
	png, err := base64.StdEncoding.DecodeString(a.Content)
	if err != nil || a.ContentID != "ingresso-A1" || a.ContentType != "image/png" || !strings.HasPrefix(string(png), "\x89PNG") {
		t.Fatalf("anexo: %+v (%v)", a.ContentID, err)
	}
}

// TestResend_Classificacao cobre CA02: cota → ErrCotaEsgotada; recusa →
// ErrEnvioPermanente; taxa/5xx/concorrente/timeout → transitório; o erro
// nunca carrega o corpo da resposta (pode ecoar o destinatário).
func TestResend_Classificacao(t *testing.T) {
	casos := []struct {
		nome        string
		f           *resendFalso
		cota, perma bool
	}{
		{"cota diária", &resendFalso{status: 429, nome: "daily_quota_exceeded"}, true, false},
		{"cota mensal", &resendFalso{status: 429, nome: "monthly_quota_exceeded"}, true, false},
		{"taxa", &resendFalso{status: 429, nome: "rate_limit_exceeded"}, false, false},
		{"validação", &resendFalso{status: 422, nome: "validation_error"}, false, true},
		{"chave inválida", &resendFalso{status: 403, nome: "invalid_api_key"}, false, true},
		{"idempotência com outro corpo", &resendFalso{status: 409, nome: "invalid_idempotent_request"}, false, true},
		{"idempotência concorrente", &resendFalso{status: 409, nome: "concurrent_idempotent_requests"}, false, false},
		{"servidor", &resendFalso{status: 503, nome: "service_unavailable"}, false, false},
	}
	for _, c := range casos {
		err := resendTeste(t, c.f).Enviar(context.Background(), mensagemTeste(t))
		if err == nil || errors.Is(err, ErrCotaEsgotada) != c.cota || errors.Is(err, ErrEnvioPermanente) != c.perma {
			t.Errorf("%s: %v", c.nome, err)
		}
		if err != nil && strings.Contains(err.Error(), "ana@exemplo.com") {
			t.Errorf("%s: erro vaza o corpo da resposta: %v", c.nome, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := resendTeste(t, &resendFalso{status: 200, lento: 500 * time.Millisecond}).Enviar(ctx, mensagemTeste(t))
	if err == nil || errors.Is(err, ErrEnvioPermanente) || errors.Is(err, ErrCotaEsgotada) {
		t.Fatalf("timeout deveria ser transitório: %v", err)
	}
	if _, err := NovoResend(ConfigResend{Remetente: "x"}); err == nil {
		t.Fatal("sem chave deveria falhar")
	}
}

// TestConsumidor_Resultados cobre CA03: classe do erro → resultado da
// métrica e decisão ack/DLQ/redelivery.
func TestConsumidor_Resultados(t *testing.T) {
	casos := []struct {
		err       error
		resultado string
		ack, perm bool
	}{
		{nil, ResultadoOK, true, false},
		{ErrNaoNotificavel, ResultadoIgnorado, true, false},
		{ErrPedidoInexistente, ResultadoPermanente, false, true},
		{ErrEnvioPermanente, ResultadoPermanente, false, true},
		{ErrCotaEsgotada, ResultadoCota, false, true},
		{errors.New("rede"), ResultadoTransitorio, false, false},
	}
	for _, c := range casos {
		var tipo, resultado string
		cons := NovoConsumidor(Config{
			Entregar: func(context.Context, uuid.UUID) error { return c.err },
			Resultado: func(_ context.Context, ti, r string, _ time.Duration) {
				tipo, resultado = ti, r
			},
		}, zap.NewNop())
		err := cons.Efeito(context.Background(), nil, outbox.Mensagem{Payload: []byte(`{"pedido_id":"` + uuid.NewString() + `"}`)})
		if tipo != TipoConfirmacao || resultado != c.resultado || (err == nil) != c.ack || errors.Is(err, outbox.ErrPermanente) != c.perm {
			t.Errorf("%v: tipo=%s resultado=%s err=%v", c.err, tipo, resultado, err)
		}
	}
}
