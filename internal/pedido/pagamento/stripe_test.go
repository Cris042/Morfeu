package pagamento

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86/webhook"
)

// Tudo sem rede (refinamento E6): o Stripe é um servidor falso local e a
// assinatura do webhook é gerada no próprio teste com um segredo de teste.

const segredoTeste = "whsec_teste_local"

func assinar(corpo []byte, segredo string, instante time.Time) string {
	return webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: corpo, Secret: segredo, Timestamp: instante}).Header
}

func eventoPI(tipo string, pedido uuid.UUID, valor int64) []byte {
	b, _ := json.Marshal(map[string]any{
		"id": "evt_" + uuid.NewString()[:8], "object": "event", "type": tipo, "api_version": "2020-01-01",
		"data": map[string]any{"object": map[string]any{
			"id": "pi_123", "object": "payment_intent", "amount": valor, "currency": "brl",
			"metadata": map[string]string{"pedido_id": pedido.String()},
		}},
	})
	return b
}

func TestWebhook_Verificar(t *testing.T) {
	w, err := NovoWebhook(segredoTeste)
	if err != nil {
		t.Fatal(err)
	}
	pedido := uuid.New()
	corpo := eventoPI("payment_intent.succeeded", pedido, 6000)
	agora := time.Now()

	ev, err := w.Verificar(corpo, assinar(corpo, segredoTeste, agora))
	if err != nil || ev.Tipo != PagamentoAprovado || ev.PedidoID != pedido || ev.ValorCentavos != 6000 ||
		ev.Moeda != "brl" || ev.IntencaoID != "pi_123" || !strings.HasPrefix(ev.ID, "evt_") {
		t.Fatalf("evento válido: %+v %v", ev, err)
	}

	adulterado := append([]byte(nil), corpo...)
	adulterado[len(adulterado)-2] ^= 1
	var v any
	_ = json.Unmarshal(corpo, &v)
	reformatado, _ := json.MarshalIndent(v, "", "  ") // mesmo conteúdo, bytes diferentes
	casos := map[string]struct {
		corpo      []byte
		assinatura string
	}{
		"sem cabeçalho":        {corpo, ""},
		"cabeçalho malformado": {corpo, "t=abc,v1=zz"},
		"outro segredo":        {corpo, assinar(corpo, "whsec_outro", agora)},
		"1 byte adulterado":    {adulterado, assinar(corpo, segredoTeste, agora)},
		"JSON reformatado":     {reformatado, assinar(corpo, segredoTeste, agora)},
		"timestamp antigo":     {corpo, assinar(corpo, segredoTeste, agora.Add(-10*time.Minute))},
		"timestamp no futuro":  {corpo, assinar(corpo, segredoTeste, agora.Add(10*time.Minute))},
	}
	for nome, c := range casos {
		if _, err := w.Verificar(c.corpo, c.assinatura); !errors.Is(err, ErrAssinaturaInvalida) {
			t.Errorf("%s: err = %v", nome, err)
		}
	}

	recusa := eventoPI("payment_intent.payment_failed", pedido, 6000)
	if ev, err := w.Verificar(recusa, assinar(recusa, segredoTeste, agora)); err != nil || ev.Tipo != PagamentoRecusado {
		t.Errorf("recusado: %+v %v", ev, err)
	}
	outro := eventoPI("charge.refunded", pedido, 6000)
	if ev, err := w.Verificar(outro, assinar(outro, segredoTeste, agora)); err != nil || ev.Tipo != Ignorado {
		t.Errorf("desconhecido: %+v %v", ev, err)
	}
	if _, err := NovoWebhook(""); err == nil {
		t.Error("segredo vazio deveria ser recusado")
	}
}

// quedaDeConexao programa o servidor falso para derrubar a conexão (falha de rede).
const quedaDeConexao = -1

// stripeFalso responde à criação de PaymentIntent com o status programado.
type stripeFalso struct {
	mu     sync.Mutex
	status []int // status por chamada; após o fim, 200
	reqs   []*http.Request
	forms  []map[string]string
	n      atomic.Int32
}

func (f *stripeFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	form := map[string]string{}
	for k := range r.PostForm {
		form[k] = r.PostForm.Get(k)
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	f.forms = append(f.forms, form)
	i := int(f.n.Add(1)) - 1
	st := http.StatusOK
	if i < len(f.status) {
		st = f.status[i]
	}
	f.mu.Unlock()
	if st == quedaDeConexao {
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(st)
	if st != http.StatusOK {
		_, _ = fmt.Fprint(w, `{"error":{"type":"api_error","code":"teste","message":"falha programada"}}`)
		return
	}
	_, _ = fmt.Fprint(w, `{"id":"pi_falso","object":"payment_intent","client_secret":"pi_falso_secret_x","amount":6000,"currency":"brl"}`)
}

func novoStripeTeste(t *testing.T, f *stripeFalso, agora func() time.Time) (*Stripe, *[]bool) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	var estados []bool
	s, err := NovoStripe(ConfigStripe{Chave: "sk_test_x", URL: srv.URL, Agora: agora, AoMudarBreaker: func(a bool) { estados = append(estados, a) }})
	if err != nil {
		t.Fatal(err)
	}
	return s, &estados
}

func TestStripe_CriarCobranca(t *testing.T) {
	f := &stripeFalso{}
	s, _ := novoStripeTeste(t, f, nil)
	pedido := uuid.New()
	in, err := s.CriarCobranca(context.Background(), Cobranca{PedidoID: pedido, ValorCentavos: 6000, Moeda: "brl", ChaveIdempotencia: "pedido-x-cobranca"})
	if err != nil || in.ID != "pi_falso" || in.SegredoCliente != "pi_falso_secret_x" {
		t.Fatalf("criar: %+v %v", in, err)
	}
	form, req := f.forms[0], f.reqs[0]
	if form["amount"] != "6000" || form["currency"] != "brl" || form["metadata[pedido_id]"] != pedido.String() ||
		form["automatic_payment_methods[enabled]"] != "true" || req.Header.Get("Idempotency-Key") != "pedido-x-cobranca" ||
		!strings.HasSuffix(req.URL.Path, "/v1/payment_intents") {
		t.Fatalf("requisição: %s %v %v", req.URL.Path, form, req.Header)
	}
}

func TestStripe_RecusaChaveDeProducao(t *testing.T) {
	for _, chave := range []string{"", "sk_live_x", "rk_live_x", "pk_test_x"} {
		if _, err := NovoStripe(ConfigStripe{Chave: chave}); !errors.Is(err, ErrChaveInvalida) {
			t.Errorf("%q: %v", chave, err)
		}
	}
	if _, err := NovoStripe(ConfigStripe{Chave: "rk_test_x"}); err != nil {
		t.Errorf("rk_test_: %v", err)
	}
}

// Retries do SDK: falha de rede é repetida com a MESMA chave (até 3
// tentativas); erro da API (4xx/5xx) não é repetido pelo SDK e 4xx não conta
// no breaker.
func TestStripe_FalhasERetries(t *testing.T) {
	f := &stripeFalso{status: []int{quedaDeConexao, quedaDeConexao}}
	s, _ := novoStripeTeste(t, f, nil)
	if _, err := s.CriarCobranca(context.Background(), Cobranca{PedidoID: uuid.New(), ValorCentavos: 1, Moeda: "brl", ChaveIdempotencia: "k1"}); err != nil {
		t.Fatalf("3ª tentativa deveria vencer: %v", err)
	}
	if len(f.reqs) != 3 || f.reqs[0].Header.Get("Idempotency-Key") != "k1" || f.reqs[2].Header.Get("Idempotency-Key") != "k1" {
		t.Fatalf("retries: %d", len(f.reqs))
	}
	f5 := &stripeFalso{status: []int{500}}
	s5, _ := novoStripeTeste(t, f5, nil)
	if _, err := s5.CriarCobranca(context.Background(), Cobranca{PedidoID: uuid.New(), Moeda: "brl", ChaveIdempotencia: "k5"}); !errors.Is(err, ErrIndisponivel) || s5.breaker.falhas != 1 {
		t.Fatalf("500 conta no breaker: %v falhas=%d", err, s5.breaker.falhas)
	}
	f2 := &stripeFalso{status: []int{400}}
	s2, _ := novoStripeTeste(t, f2, nil)
	if _, err := s2.CriarCobranca(context.Background(), Cobranca{PedidoID: uuid.New(), Moeda: "brl", ChaveIdempotencia: "k2"}); !errors.Is(err, ErrIndisponivel) {
		t.Fatalf("400: %v", err)
	}
	if s2.breaker.falhas != 0 || len(f2.reqs) != 1 {
		t.Fatalf("4xx não conta no breaker nem é repetido: falhas=%d reqs=%d", s2.breaker.falhas, len(f2.reqs))
	}
}

func TestBreaker(t *testing.T) {
	t0 := time.Date(2098, 1, 1, 12, 0, 0, 0, time.UTC)
	var estados []bool
	b := &breaker{aoMudar: func(a bool) { estados = append(estados, a) }}
	for range falhasParaAbrir - 1 {
		b.registrar(t0, false)
	}
	if !b.permitir(t0) {
		t.Fatal("abriu antes de 5 falhas")
	}
	b.registrar(t0, false)
	if b.permitir(t0.Add(breakerAbertoPor - time.Nanosecond)) {
		t.Fatal("deveria estar aberto")
	}
	meio := t0.Add(breakerAbertoPor)
	if !b.permitir(meio) {
		t.Fatal("deveria deixar passar (meio-aberto)")
	}
	b.registrar(meio, false) // falha no meio-aberto reabre na hora
	if b.permitir(meio) {
		t.Fatal("falha no meio-aberto deveria reabrir")
	}
	fim := meio.Add(breakerAbertoPor)
	b.registrar(fim, true)
	if !b.permitir(fim) || b.falhas != 0 {
		t.Fatal("sucesso deveria fechar")
	}
	if fmt.Sprint(estados) != "[true false]" {
		t.Fatalf("transições informadas: %v", estados)
	}
}

// rotasFalsas imita as rotas de consulta, cancelamento e estorno do Stripe.
type rotasFalsas struct {
	mu                      sync.Mutex
	vistos                  []string
	chaveEstorno, piEstorno string
	status                  string
}

func (f *rotasFalsas) definir(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

func (f *rotasFalsas) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.vistos = append(f.vistos, r.Method+" "+r.URL.Path)
	if r.URL.Path == "/v1/refunds" {
		f.chaveEstorno, f.piEstorno = r.Header.Get("Idempotency-Key"), r.PostForm.Get("payment_intent")
	}
	st := f.status
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v1/refunds":
		_, _ = fmt.Fprint(w, `{"id":"re_1","object":"refund","status":"succeeded"}`)
	case strings.HasSuffix(r.URL.Path, "/cancel") && st == "succeeded":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"payment_intent_unexpected_state"}}`)
	default:
		_, _ = fmt.Fprintf(w, `{"id":"pi_1","object":"payment_intent","status":%q,"amount":6000,"currency":"brl"}`, st)
	}
}

// TestStripe_ConsultarCancelarEstornar cobre a borda da reconciliação e do
// estorno (PRD 0025): caminho, método, parâmetros e idempotência.
func TestStripe_ConsultarCancelarEstornar(t *testing.T) {
	f := &rotasFalsas{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	s, err := NovoStripe(ConfigStripe{Chave: "rk_test_x", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for st, quer := range map[string]EstadoCobranca{"succeeded": CobrancaAprovada, "canceled": CobrancaCancelada, "requires_payment_method": CobrancaPendente, "processing": CobrancaPendente} {
		f.definir(st)
		sit, err := s.ConsultarCobranca(ctx, "pi_1")
		if err != nil || sit != (Situacao{Estado: quer, ValorCentavos: 6000, Moeda: "brl"}) {
			t.Errorf("%s: %+v %v", st, sit, err)
		}
	}
	f.definir("succeeded")
	if err := s.CancelarCobranca(ctx, "pi_1"); !errors.Is(err, ErrIndisponivel) || s.breaker.falhas != 0 {
		t.Fatalf("cancelar aprovada: %v (a recusa 4xx não conta no breaker: %d)", err, s.breaker.falhas)
	}
	f.definir("requires_payment_method")
	if err := s.CancelarCobranca(ctx, "pi_1"); err != nil {
		t.Fatalf("cancelar pendente: %v", err)
	}
	if err := s.Estornar(ctx, "pi_1", "estorno-abc"); err != nil || f.chaveEstorno != "estorno-abc" || f.piEstorno != "pi_1" {
		t.Fatalf("estornar: %v chave=%q pi=%q", err, f.chaveEstorno, f.piEstorno)
	}
	if !slices.Contains(f.vistos, "GET /v1/payment_intents/pi_1") || !slices.Contains(f.vistos, "POST /v1/payment_intents/pi_1/cancel") {
		t.Fatalf("rotas: %v", f.vistos)
	}
}

func TestFake_EstadoDasCobrancas(t *testing.T) {
	f := NovoFake()
	ctx := context.Background()
	in, _ := f.CriarCobranca(ctx, Cobranca{PedidoID: uuid.New(), ValorCentavos: 3000, Moeda: "brl"})
	if sit, _ := f.ConsultarCobranca(ctx, in.ID); sit.Estado != CobrancaPendente || sit.ValorCentavos != 3000 {
		t.Fatalf("nova: %+v", sit)
	}
	f.Aprovar(in.ID)
	if err := f.CancelarCobranca(ctx, in.ID); err == nil {
		t.Fatal("aprovada não se cancela")
	}
	f.FalharEstornos(1)
	if err := f.Estornar(ctx, in.ID, "k"); !errors.Is(err, ErrIndisponivel) {
		t.Fatalf("estorno programado para falhar: %v", err)
	}
	if err := f.Estornar(ctx, in.ID, "k"); err != nil || fmt.Sprint(f.Estornos()) != "[k k]" {
		t.Fatalf("estorno: %v %v", err, f.Estornos())
	}
	outra, _ := f.CriarCobranca(ctx, Cobranca{PedidoID: uuid.New(), ValorCentavos: 1, Moeda: "brl"})
	if err := f.CancelarCobranca(ctx, outra.ID); err != nil || f.Cancelamentos()[0] != outra.ID {
		t.Fatalf("cancelar: %v", err)
	}
	if _, err := f.ConsultarCobranca(ctx, "pi_nada"); !errors.Is(err, ErrCobrancaDesconhecida) {
		t.Fatalf("desconhecida: %v", err)
	}
}
