//go:build integration
// +build integration

package pedido

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Consulta de convidado e página do ingresso (PRD 0034).

// pago compra 2 assentos como convidado e paga pela rota de teste.
func (a *ambiente) pago(t *testing.T) (pedidoDTO, int64) {
	t.Helper()
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID, "A1", "A2")
	r := a.reqAuth(http.MethodPost, "/pedidos", car, "", map[string]any{"email": "Ana@Exemplo.com", "sessao_id": sessaoID, "assentos": []string{"A1", "A2"}})
	p := criadoDe(t, r).Pedido
	if rr := a.reqAuth(http.MethodPost, "/__teste/pagar/"+p.ID.String(), "", "", nil); rr.code != http.StatusNoContent {
		t.Fatalf("pagar: %d %s", rr.code, rr.corpo)
	}
	return p, sessaoID
}

type consultaDTO struct {
	Pedido    pedidoDTO `json:"pedido"`
	Ingressos []struct {
		Assento string `json:"assento"`
		Ref     string `json:"ref"`
	} `json:"ingressos"`
}

func (a *ambiente) consultar(email, codigo string) resposta {
	return a.req(http.MethodPost, "/pedidos/consulta", "", map[string]string{"email": email, "codigo": codigo}, true)
}

// TestConsulta cobre CA01/CA02: acerto devolve pedido + links; todo "não
// encontrado" é byte a byte igual (status, corpo e headers).
func TestConsulta(t *testing.T) {
	a := novoAmbiente(t)
	p, _ := a.pago(t)

	// E-mail normalizado e código como o cliente digita (minúsculas, hífen).
	cod := strings.ToLower(p.Codigo[:8]) + "-" + p.Codigo[8:]
	r := a.consultar("  ana@exemplo.COM ", cod)
	var c consultaDTO
	if r.code != http.StatusOK || json.Unmarshal([]byte(r.corpo), &c) != nil || c.Pedido.ID != p.ID || c.Pedido.Status != Pago ||
		len(c.Ingressos) != 2 || r.cabecalho.Get("Cache-Control") != "no-store" {
		t.Fatalf("consulta: %d %s", r.code, r.corpo)
	}
	for _, i := range c.Ingressos {
		id, tok, ok := separarRef(i.Ref)
		if !ok || tok != TokenIngresso(segredoTokenTeste, id) {
			t.Fatalf("ref inválida: %s", i.Ref)
		}
	}
	if strings.Contains(r.corpo, "secret") || strings.Contains(r.corpo, "pi_") {
		t.Fatalf("consulta vaza a cobrança: %s", r.corpo)
	}

	base := a.consultar("outra@exemplo.com", p.Codigo)
	if base.code != http.StatusNotFound {
		t.Fatalf("e-mail errado: %d", base.code)
	}
	for nome, r := range map[string]resposta{
		"código inexistente": a.consultar("ana@exemplo.com", "AAAAAAAAAAAAAAAA"),
		"código malformado":  a.consultar("ana@exemplo.com", "x"),
		"código vazio":       a.consultar("", ""),
	} {
		if r.code != base.code || r.corpo != base.corpo || !mesmosCabecalhos(r.cabecalho, base.cabecalho) {
			t.Errorf("%s distinguível: %d %s %v × %d %s %v", nome, r.code, r.corpo, r.cabecalho, base.code, base.corpo, base.cabecalho)
		}
	}

	// Pedido não pago: consulta devolve o pedido, sem links.
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID, "B1")
	pend := criadoDe(t, a.reqAuth(http.MethodPost, "/pedidos", car, "", map[string]any{"email": "bia@exemplo.com", "sessao_id": sessaoID, "assentos": []string{"B1"}})).Pedido
	r = a.consultar("bia@exemplo.com", pend.Codigo)
	if r.code != http.StatusOK || json.Unmarshal([]byte(r.corpo), &c) != nil || len(c.Ingressos) != 0 {
		t.Fatalf("pendente: %d %s", r.code, r.corpo)
	}
	if rr := a.req(http.MethodPost, "/pedidos/consulta", "", map[string]string{"email": "x", "codigo": "y"}, false); rr.code != http.StatusForbidden {
		t.Fatalf("sem anti-CSRF: %d", rr.code)
	}
}

func mesmosCabecalhos(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if a.Get(k) != b.Get(k) {
			return false
		}
	}
	return true
}

// TestConsulta_Limite cobre CA03: teto por e-mail (normalizado) e por IP.
func TestConsulta_Limite(t *testing.T) {
	a := montarAmbiente(t, limites{ip: 100000, dono: 100000, consulta: 2})
	for range 2 {
		if r := a.consultar("Zed@exemplo.com", "AAAAAAAAAAAAAAAA"); r.code != http.StatusNotFound {
			t.Fatalf("antes do teto: %d", r.code)
		}
	}
	if r := a.consultar(" zed@EXEMPLO.com", "AAAAAAAAAAAAAAAA"); r.code != http.StatusTooManyRequests {
		t.Fatalf("teto por e-mail: %d", r.code)
	}
}

func (a *ambiente) ingresso(caminho string) resposta {
	return a.req(http.MethodGet, caminho, "", nil, false)
}

// TestIngresso cobre CA04–CA06: link válido → dados + QR com o link do
// e-mail; inválido/inexistente/token errado → 404 idêntico; 410 só depois do
// HMAC válido; headers em todas as respostas.
func TestIngresso(t *testing.T) {
	a := novoAmbiente(t)
	p, _ := a.pago(t)
	var c consultaDTO
	_ = json.Unmarshal([]byte(a.consultar("ana@exemplo.com", p.Codigo).corpo), &c)
	ref := c.Ingressos[0].Ref

	r := a.ingresso("/i/" + ref)
	var dto struct {
		Assento, Status, Filme, Sala string
		Inicio                       time.Time
	}
	if r.code != http.StatusOK || json.Unmarshal([]byte(r.corpo), &dto) != nil || dto.Assento != "A1" || dto.Status != "ativo" ||
		dto.Filme != "Filme de teste" || !dto.Inicio.Equal(time.Date(2099, 1, 1, 20, 0, 0, 0, time.UTC)) {
		t.Fatalf("ingresso: %d %s", r.code, r.corpo)
	}
	conferirCabecalhos(t, "200", r)
	qr := a.ingresso("/i/" + ref + "/qr.png")
	if qr.code != http.StatusOK || qr.corpo != "PNG:"+baseURLTeste+"/i/"+ref || qr.cabecalho.Get("Content-Type") != "image/png" {
		t.Fatalf("qr: %d %s", qr.code, qr.corpo)
	}
	conferirCabecalhos(t, "qr", qr)

	id, tok, _ := separarRef(ref)
	outroID := uuid.New()
	base := a.ingresso("/i/" + outroID.String() + "." + TokenIngresso(segredoTokenTeste, outroID)) // HMAC certo, id inexistente
	if base.code != http.StatusNotFound {
		t.Fatalf("inexistente: %d", base.code)
	}
	trocado := tok[:42] + map[bool]string{true: "B", false: "A"}[tok[42] == 'A']
	for nome, caminho := range map[string]string{
		"token errado":         "/i/" + id.String() + "." + trocado,
		"token de outro id":    "/i/" + id.String() + "." + TokenIngresso(segredoTokenTeste, outroID),
		"segredo errado":       "/i/" + id.String() + "." + TokenIngresso([]byte("outro-segredo-de-teste-32-bytes!"), id),
		"sem ponto":            "/i/" + id.String() + tok,
		"uuid maiúsculo":       "/i/" + strings.ToUpper(id.String()) + "." + tok,
		"token curto":          "/i/" + id.String() + "." + tok[:40],
		"token com padding":    "/i/" + id.String() + "." + tok[:42] + "=",
		"lixo":                 "/i/x",
		"qr com token errado":  "/i/" + id.String() + "." + trocado + "/qr.png",
		"qr de id inexistente": "/i/" + outroID.String() + "." + TokenIngresso(segredoTokenTeste, outroID) + "/qr.png",
	} {
		r := a.ingresso(caminho)
		if r.code != base.code || r.corpo != base.corpo || !mesmosCabecalhos(r.cabecalho, base.cabecalho) {
			t.Errorf("%s distinguível: %d %s", nome, r.code, r.corpo)
		}
	}
	conferirCabecalhos(t, "404", base)

	// Expirado (início + 24 h): 410 só com o token certo; token errado segue 404.
	a.rel.t = time.Date(2099, 1, 2, 20, 0, 1, 0, time.UTC)
	exp := a.ingresso("/i/" + ref)
	if exp.code != http.StatusGone {
		t.Fatalf("expirado: %d %s", exp.code, exp.corpo)
	}
	conferirCabecalhos(t, "410", exp)
	if r := a.ingresso("/i/" + id.String() + "." + trocado); r.code != http.StatusNotFound {
		t.Fatalf("expirado com token errado virou oráculo: %d", r.code)
	}
}

// TestIngresso_Revogado cobre CA05: ingresso cancelado → 410 (token válido).
func TestIngresso_Revogado(t *testing.T) {
	a := novoAmbiente(t)
	p, _ := a.pago(t)
	var c consultaDTO
	_ = json.Unmarshal([]byte(a.consultar("ana@exemplo.com", p.Codigo).corpo), &c)
	if _, err := pool.Exec(context.Background(), `UPDATE ingressos SET status = 'cancelado' WHERE pedido_id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if r := a.ingresso("/i/" + c.Ingressos[0].Ref); r.code != http.StatusGone {
		t.Fatalf("cancelado: %d", r.code)
	}
	if r := a.ingresso("/i/" + c.Ingressos[0].Ref + "/qr.png"); r.code != http.StatusGone {
		t.Fatalf("qr cancelado: %d", r.code)
	}
	// Cancelado some da consulta (só ingressos ativos têm link).
	_ = json.Unmarshal([]byte(a.consultar("ana@exemplo.com", p.Codigo).corpo), &c)
	if len(c.Ingressos) != 0 {
		t.Fatalf("cancelado com link: %+v", c.Ingressos)
	}
}

// TestIngresso_VersaoDoToken cobre CA06: ingresso de outra versão valida com
// o segredo dela; versão sem segredo configurado não vira 404 silencioso.
func TestIngresso_VersaoDoToken(t *testing.T) {
	a := novoAmbiente(t)
	p, _ := a.pago(t)
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `UPDATE ingressos SET versao_token = 2 WHERE pedido_id = $1 AND assento_codigo = 'A1' RETURNING id`, p.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	v2 := []byte("segredo-da-versao-2-de-teste-32b")
	a.servico.cfg.SegredosToken[2] = v2
	t.Cleanup(func() { delete(a.servico.cfg.SegredosToken, 2) })
	if r := a.ingresso("/i/" + id.String() + "." + TokenIngresso(v2, id)); r.code != http.StatusOK {
		t.Fatalf("versão 2: %d %s", r.code, r.corpo)
	}
	if r := a.ingresso("/i/" + id.String() + "." + TokenIngresso(segredoTokenTeste, id)); r.code != http.StatusNotFound {
		t.Fatalf("token da v1 num ingresso v2: %d", r.code)
	}
	// Versão aposentada (sem segredo): 404, nunca 500 (não revela o id).
	delete(a.servico.cfg.SegredosToken, 2)
	for _, tok := range []string{TokenIngresso(v2, id), TokenIngresso(segredoTokenTeste, id)} {
		if r := a.ingresso("/i/" + id.String() + "." + tok); r.code != http.StatusNotFound {
			t.Fatalf("versão sem segredo: %d %s", r.code, r.corpo)
		}
	}
}

// TestIngresso_Limite cobre CA07: teto por IP na página e no QR.
func TestIngresso_Limite(t *testing.T) {
	a := montarAmbiente(t, limites{ip: 100000, dono: 100000, consulta: 2})
	a.ingresso("/i/x")
	a.ingresso("/i/y/qr.png")
	if r := a.ingresso("/i/z"); r.code != http.StatusTooManyRequests || r.cabecalho.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("teto por IP: %d %v", r.code, r.cabecalho)
	}
}

func conferirCabecalhos(t *testing.T, caso string, r resposta) {
	t.Helper()
	h := r.cabecalho
	if h.Get("Referrer-Policy") != "no-referrer" || h.Get("Cache-Control") != "no-store" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("%s: headers %v", caso, h)
	}
}
