//go:build integration
// +build integration

package pedido

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/reserva"
)

// Conta no checkout, "Meus pedidos", retomada e pagamento de teste (PRD 0031).

// reqAuth faz a requisição com carrinho e Bearer opcionais (e anti-CSRF).
func (a *ambiente) reqAuth(metodo, caminho, carrinho, token string, corpo any) resposta {
	var body []byte
	if corpo != nil {
		body, _ = json.Marshal(corpo)
	}
	r := httptest.NewRequest(metodo, caminho, bytes.NewReader(body))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	r.Header.Set(headerAntiCSRF, valorAntiCSRF)
	if carrinho != "" {
		r.AddCookie(&http.Cookie{Name: cookieCarrinho, Value: carrinho}) //nolint:gosec // cookie de requisição no teste
	}
	if token != "" {
		r.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, r)
	return resposta{code: rec.Code, corpo: strings.TrimSpace(rec.Body.String()), cabecalho: rec.Header()}
}

func (a *ambiente) tokenDe(t *testing.T, id uuid.UUID) string {
	t.Helper()
	tok, _, err := a.emissor.Emitir(id, autenticacao.PapelCliente)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// comprar cria sessão + carrinho com o assento travado e abre o pedido,
// logado (token não vazio) ou como convidado.
func (a *ambiente) comprar(t *testing.T, token string) (resposta, string) {
	t.Helper()
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID, "A1")
	return a.reqAuth(http.MethodPost, "/pedidos", car, token, map[string]any{"email": "ana@exemplo.com", "sessao_id": sessaoID, "assentos": []string{"A1"}}), car
}

func usuarioDoPedido(t *testing.T, id uuid.UUID) *uuid.UUID {
	t.Helper()
	var u *uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT usuario_id FROM pedidos WHERE id = $1`, id).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return u
}

// TestConta_Vinculo cobre CA01: usuario_id só do JWT; convidado nulo; Bearer
// inválido → 401 sem pedido (nunca cai para convidado).
func TestConta_Vinculo(t *testing.T) {
	a := novoAmbiente(t)
	ana := uuid.New()
	r, _ := a.comprar(t, a.tokenDe(t, ana))
	if u := usuarioDoPedido(t, criadoDe(t, r).Pedido.ID); u == nil || *u != ana {
		t.Fatalf("logado sem vínculo: %v", u)
	}
	r, _ = a.comprar(t, "")
	if u := usuarioDoPedido(t, criadoDe(t, r).Pedido.ID); u != nil {
		t.Fatalf("convidado vinculado: %v", u)
	}
	r, car := a.comprar(t, "token-invalido")
	if r.code != http.StatusUnauthorized {
		t.Fatalf("Bearer inválido: %d %s", r.code, r.corpo)
	}
	d, _ := reservaDonoHash(car)
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM pedidos WHERE dono_hash = $1`, d).Scan(&n)
	if n != 0 {
		t.Fatal("Bearer inválido não pode criar pedido")
	}
}

// TestConta_MeusPedidos cobre CA02: lista só os da conta, mais recentes
// primeiro; leitura do pedido pela conta sem o carrinho; alheio → 404.
func TestConta_MeusPedidos(t *testing.T) {
	a := novoAmbiente(t)
	ana, bia := uuid.New(), uuid.New()
	tokAna, tokBia := a.tokenDe(t, ana), a.tokenDe(t, bia)
	r1, _ := a.comprar(t, tokAna)
	p1 := criadoDe(t, r1).Pedido
	a.rel.avancar(TTLPedido) // o 2º pedido nasce depois
	r2, _ := a.comprar(t, tokAna)
	p2 := criadoDe(t, r2).Pedido
	r3, _ := a.comprar(t, tokBia)
	p3 := criadoDe(t, r3).Pedido

	r := a.reqAuth(http.MethodGet, "/pedidos", "", tokAna, nil)
	var lista struct {
		Pedidos []pedidoDTO `json:"pedidos"`
	}
	if r.code != http.StatusOK || json.Unmarshal([]byte(r.corpo), &lista) != nil || len(lista.Pedidos) != 2 ||
		lista.Pedidos[0].ID != p2.ID || lista.Pedidos[1].ID != p1.ID || r.cabecalho.Get(echo.HeaderCacheControl) != "no-store" {
		t.Fatalf("meus pedidos: %d %s", r.code, r.corpo)
	}
	if r := a.reqAuth(http.MethodGet, "/pedidos", "", "", nil); r.code != http.StatusUnauthorized {
		t.Fatalf("sem login: %d", r.code)
	}
	if r := a.reqAuth(http.MethodGet, "/pedidos/"+p1.ID.String(), "", tokAna, nil); r.code != http.StatusOK {
		t.Fatalf("pedido pela conta: %d %s", r.code, r.corpo)
	}
	if r := a.reqAuth(http.MethodGet, "/pedidos/"+p3.ID.String(), "", tokAna, nil); r.code != http.StatusNotFound {
		t.Fatalf("pedido alheio: %d", r.code)
	}
}

// TestRetomar cobre CA03: só o carrinho dono, só aguardando no prazo;
// segredo vindo do gateway, sem cache.
func TestRetomar(t *testing.T) {
	a := novoAmbiente(t)
	r, car := a.comprar(t, "")
	p := criadoDe(t, r).Pedido
	caminho := "/pedidos/" + p.ID.String() + "/retomar"
	rr := a.reqAuth(http.MethodPost, caminho, car, "", nil)
	var ret struct {
		ClientSecret string `json:"client_secret"`
		Codigo       string `json:"codigo"`
	}
	if rr.code != http.StatusOK || json.Unmarshal([]byte(rr.corpo), &ret) != nil ||
		ret.ClientSecret != "pi_fake_"+p.ID.String()+"_secret_fake" || ret.Codigo != p.Codigo || rr.cabecalho.Get(echo.HeaderCacheControl) != "no-store" {
		t.Fatalf("retomar: %d %s", rr.code, rr.corpo)
	}
	if r := a.reqAuth(http.MethodPost, caminho, a.carrinho(t, novaSessao(t)), "", nil); r.code != http.StatusNotFound {
		t.Fatalf("outro carrinho: %d", r.code)
	}
	if r := a.reqAuth(http.MethodPost, caminho, car, "", nil); r.code != http.StatusOK {
		t.Fatalf("2ª retomada: %d", r.code)
	}
	a.rel.avancar(TTLPedido)
	if r := a.reqAuth(http.MethodPost, caminho, car, "", nil); r.code != http.StatusNotFound {
		t.Fatalf("expirado: %d", r.code)
	}
}

// TestPagarParaTeste cobre CA04: a rota de teste passa pelo webhook real
// (assinatura + pivô) e só existe quando registrada (gateway fake).
func TestPagarParaTeste(t *testing.T) {
	a := novoAmbiente(t)
	r, car := a.comprar(t, "")
	p := criadoDe(t, r).Pedido
	if rr := a.reqAuth(http.MethodPost, "/__teste/pagar/"+p.ID.String(), "", "", nil); rr.code != http.StatusNoContent {
		t.Fatalf("pagar: %d %s", rr.code, rr.corpo)
	}
	if e := efeitosDe(t, p.ID); e != (efeitos{status: "pago", ingressos: 1, outbox: 1, convertidos: 1}) {
		t.Fatalf("efeitos: %+v", e)
	}
	if rr := a.reqAuth(http.MethodPost, "/pedidos/"+p.ID.String()+"/retomar", car, "", nil); rr.code != http.StatusNotFound {
		t.Fatalf("pago não retoma: %d", rr.code)
	}

	// Sem ComRotasDeTeste (gateway real no main): a rota não existe.
	e := echo.New()
	NovoHandler(a.servico, zap.NewNop()).RegistrarRotas(e)
	req := httptest.NewRequest(http.MethodPost, "/__teste/pagar/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("rota de teste sem registro: %d", rec.Code)
	}
}

// reservaDonoHash devolve o hash do carrinho (o que o pedido guarda).
func reservaDonoHash(carrinho string) ([]byte, bool) {
	d, ok := reserva.DonoDoToken(carrinho)
	if !ok {
		return nil, false
	}
	return d.Hash(), true
}
