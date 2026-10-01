//go:build integration
// +build integration

package pedido

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mclovin137/morfeu/internal/reserva"
)

// Cancelamento pelo cliente e da sessão com vendidos (PRD 0036, ADR 0011).

// inicioSessaoTeste é o início das sessões de novaSessao.
var inicioSessaoTeste = time.Date(2099, 1, 1, 20, 0, 0, 0, time.UTC)

// irPara leva o relógio da suíte ao instante t.
func (a *ambiente) irPara(t time.Time) { a.rel.avancar(t.Sub(a.rel.agora())) }

// pagoDaConta compra 1 assento logado e paga pela rota de teste.
func (a *ambiente) pagoDaConta(t *testing.T, token string) pedidoDTO {
	t.Helper()
	r, _ := a.comprar(t, token)
	p := criadoDe(t, r).Pedido
	a.pagar(t, p.ID)
	return p
}

func (a *ambiente) pagar(t *testing.T, id uuid.UUID) {
	t.Helper()
	if rr := a.reqAuth(http.MethodPost, "/__teste/pagar/"+id.String(), "", "", nil); rr.code != http.StatusNoContent {
		t.Fatalf("pagar: %d %s", rr.code, rr.corpo)
	}
}

func (a *ambiente) cancelarDaConta(id uuid.UUID, token string) resposta {
	return a.reqAuth(http.MethodPost, "/pedidos/"+id.String()+"/cancelar", "", token, nil)
}

func (a *ambiente) cancelarConvidado(email, codigo string) resposta {
	return a.req(http.MethodPost, "/pedidos/consulta/cancelar", "", map[string]string{"email": email, "codigo": codigo}, true)
}

func pedidoDe(t *testing.T, r resposta) pedidoDTO {
	t.Helper()
	var p pedidoDTO
	if r.code != http.StatusOK || json.Unmarshal([]byte(r.corpo), &p) != nil {
		t.Fatalf("pedido: %d %s", r.code, r.corpo)
	}
	return p
}

// vezes conta as chamadas de estorno com a chave (o fake registra também as
// de pedidos de outros testes, que falham por serem de outro fake).
func vezes(chaves []string, chave string) int {
	n := 0
	for _, c := range chaves {
		if c == chave {
			n++
		}
	}
	return n
}

func contarNoBanco(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestCancelar_Conta cobre CA03.
func TestCancelar_Conta(t *testing.T) {
	a := novoAmbiente(t)
	ana := uuid.New()
	tok := a.tokenDe(t, ana)
	p := a.pagoDaConta(t, tok)
	if v := pedidoDe(t, a.reqAuth(http.MethodGet, "/pedidos/"+p.ID.String(), "", tok, nil)); !v.Cancelavel {
		t.Fatalf("pago dentro da janela deveria ser cancelável: %+v", v)
	}
	ref := refDoPrimeiroIngresso(t, p.ID)

	v := pedidoDe(t, a.cancelarDaConta(p.ID, tok))
	if v.Status != EstornoPendente || v.Cancelavel {
		t.Fatalf("cancelado: %+v", v)
	}
	if e := efeitosDe(t, p.ID); e.motivo != MotivoCancelamento || e.ingressos != 0 {
		t.Fatalf("efeitos: %+v", e)
	}
	if r := a.ingresso("/i/" + ref); r.code != http.StatusGone {
		t.Fatalf("ingresso de pedido cancelado deveria ser 410: %d %s", r.code, r.corpo)
	}
	trilhaAntes := trilha(t, p.ID)
	if trilhaAntes != "->aguardando_pagamento aguardando_pagamento>pago pago>estorno_pendente" {
		t.Fatalf("trilha: %s", trilhaAntes)
	}

	// Idempotente: 200 com o estado atual, nada novo na trilha nem na métrica.
	if v := pedidoDe(t, a.cancelarDaConta(p.ID, tok)); v.Status != EstornoPendente || trilha(t, p.ID) != trilhaAntes {
		t.Fatalf("repetir: %+v %s", v, trilha(t, p.ID))
	}
	if n := lerContador(a.cancel, OrigemCliente); n != 1 {
		t.Fatalf("cancelamentos_total{cliente}: %d", n)
	}

	// Outra conta → 404 idêntico ao inexistente; sem token → 401; sem anti-CSRF → 403.
	alheio := a.cancelarDaConta(p.ID, a.tokenDe(t, uuid.New()))
	inexistente := a.cancelarDaConta(uuid.New(), tok)
	if alheio.code != http.StatusNotFound || alheio.corpo != inexistente.corpo || inexistente.code != http.StatusNotFound {
		t.Fatalf("alheio %d %s × inexistente %d %s", alheio.code, alheio.corpo, inexistente.code, inexistente.corpo)
	}
	if r := a.cancelarDaConta(p.ID, ""); r.code != http.StatusUnauthorized {
		t.Fatalf("sem token: %d", r.code)
	}
	if r := a.req(http.MethodPost, "/pedidos/"+p.ID.String()+"/cancelar", "", nil, false); r.code != http.StatusForbidden {
		t.Fatalf("sem anti-CSRF: %d", r.code)
	}
}

// refDoPrimeiroIngresso devolve o link "{id}.{token}" do 1º ingresso ativo.
func refDoPrimeiroIngresso(t *testing.T, pedidoID uuid.UUID) string {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM ingressos WHERE pedido_id = $1 AND status = 'ativo' ORDER BY assento_codigo LIMIT 1`, pedidoID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id.String() + "." + TokenIngresso(segredoTokenTeste, id)
}

// TestCancelar_Convidado cobre CA04.
func TestCancelar_Convidado(t *testing.T) {
	a := montarAmbiente(t, limites{ip: 100000, dono: 100000, consulta: 4})
	p, _ := a.pago(t)

	errado := a.cancelarConvidado("outra@exemplo.com", p.Codigo)
	consultaErrada := a.consultar("outra@exemplo.com", p.Codigo)
	if errado.code != http.StatusNotFound || errado.corpo != consultaErrada.corpo || !mesmosCabecalhos(errado.cabecalho, consultaErrada.cabecalho) {
		t.Fatalf("não encontrado deveria ser idêntico ao da consulta: %d %s × %d %s", errado.code, errado.corpo, consultaErrada.code, consultaErrada.corpo)
	}
	r := a.cancelarConvidado(" ana@exemplo.com", p.Codigo)
	var c consultaDTO
	if r.code != http.StatusOK || json.Unmarshal([]byte(r.corpo), &c) != nil || c.Pedido.Status != EstornoPendente ||
		c.Pedido.Cancelavel || len(c.Ingressos) != 0 || r.cabecalho.Get("Cache-Control") != "no-store" {
		t.Fatalf("cancelar: %d %s", r.code, r.corpo)
	}
	// O teto é o mesmo da consulta (4/min por e-mail): a 5ª chamada → 429.
	if r := a.cancelarConvidado("ana@exemplo.com", p.Codigo); r.code != http.StatusOK {
		t.Fatalf("4ª: %d", r.code)
	}
	if r := a.cancelarConvidado("ana@exemplo.com", p.Codigo); r.code != http.StatusTooManyRequests {
		t.Fatalf("5ª deveria ser 429: %d", r.code)
	}
}

// TestCancelar_Recusas cobre CA05.
func TestCancelar_Recusas(t *testing.T) {
	a := novoAmbiente(t)
	tok := a.tokenDe(t, uuid.New())

	r, _ := a.comprar(t, tok)
	pendente := criadoDe(t, r).Pedido
	if r := a.cancelarDaConta(pendente.ID, tok); r.code != http.StatusConflict || r.corpo != `{"erro":"nao_cancelavel"}` {
		t.Fatalf("aguardando: %d %s", r.code, r.corpo)
	}

	usado := a.pagoDaConta(t, tok)
	if _, err := pool.Exec(context.Background(), `UPDATE ingressos SET status = 'usado' WHERE pedido_id = $1`, usado.ID); err != nil {
		t.Fatal(err)
	}
	if r := a.cancelarDaConta(usado.ID, tok); r.code != http.StatusConflict || r.corpo != `{"erro":"nao_cancelavel"}` {
		t.Fatalf("usado: %d %s", r.code, r.corpo)
	}

	tarde := a.pagoDaConta(t, tok)
	a.irPara(inicioSessaoTeste.Add(-JanelaCancelamento).Add(time.Second))
	if v := pedidoDe(t, a.reqAuth(http.MethodGet, "/pedidos/"+tarde.ID.String(), "", tok, nil)); v.Cancelavel {
		t.Fatal("fora da janela não deveria ser cancelável")
	}
	if r := a.cancelarDaConta(tarde.ID, tok); r.code != http.StatusConflict || r.corpo != `{"erro":"fora_da_janela"}` {
		t.Fatalf("fora da janela: %d %s", r.code, r.corpo)
	}
	if statusDe(t, tarde.ID) != string(Pago) || lerContador(a.cancel, OrigemCliente) != 0 {
		t.Fatal("recusa não pode mudar o pedido nem contar cancelamento")
	}
}

// TestCancelar_CicloCompleto cobre CA06: cancelado → estorno → assentos livres.
func TestCancelar_CicloCompleto(t *testing.T) {
	a := novoAmbiente(t)
	tok := a.tokenDe(t, uuid.New())
	p := a.pagoDaConta(t, tok)
	pedidoDe(t, a.cancelarDaConta(p.ID, tok))

	// Antes do estorno o assento continua ocupado (ADR 0011).
	_, d, _ := reserva.NovoCarrinho()
	if _, err := a.reserva.Travar(context.Background(), p.SessaoID, []string{"A1"}, d); err == nil {
		t.Fatal("assento não pode voltar à venda antes do estorno")
	}
	// Estorno só deste pedido (o lote do job é global e o banco é
	// compartilhado pela suíte — isolado contra resíduos de outros testes).
	var pi *string
	_ = pool.QueryRow(context.Background(), `SELECT payment_intent_id FROM pedidos WHERE id = $1`, p.ID).Scan(&pi)
	if err := a.servico.estornarUm(context.Background(), p.ID, pi, 0); err != nil {
		t.Fatalf("estornar: %v", err)
	}
	if statusDe(t, p.ID) != string(Estornado) || vezes(a.gateway.Estornos(), ChaveEstorno(p.ID)) != 1 {
		t.Fatalf("status %s, estornos %v", statusDe(t, p.ID), a.gateway.Estornos())
	}
	if n := contarNoBanco(t, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = $2`, p.ID.String(), EventoEstornado); n != 1 {
		t.Fatalf("pedido.estornado na outbox: %d", n)
	}
	if _, err := a.reserva.Travar(context.Background(), p.SessaoID, []string{"A1"}, d); err != nil {
		t.Fatalf("assento deveria estar livre depois do estorno: %v", err)
	}
}

// TestCancelar_Concorrente cobre CA07: N cancelamentos do mesmo pedido → 1
// transição e 1 estorno no gateway.
func TestCancelar_Concorrente(t *testing.T) {
	a := novoAmbiente(t)
	tok := a.tokenDe(t, uuid.New())
	p := a.pagoDaConta(t, tok)
	const n = 10
	var wg sync.WaitGroup
	codigos := make([]int, n)
	inicio := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-inicio
			codigos[i] = a.cancelarDaConta(p.ID, tok).code
		}()
	}
	close(inicio)
	wg.Wait()
	for i, c := range codigos {
		if c != http.StatusOK {
			t.Fatalf("cancelamento %d: %d", i, c)
		}
	}
	if n := contarNoBanco(t, `SELECT count(*) FROM pedido_eventos WHERE pedido_id = $1 AND de = 'pago'`, p.ID); n != 1 {
		t.Fatalf("transições a partir de pago: %d", n)
	}
	for range 3 {
		a.servico.ExecutarEstornos(context.Background())
	}
	if got := a.gateway.Estornos(); vezes(got, ChaveEstorno(p.ID)) != 1 || lerContador(a.cancel, OrigemCliente) != 1 {
		t.Fatalf("estornos %v, métrica %d", got, lerContador(a.cancel, OrigemCliente))
	}
}

// TestCancelarSessao_ComPedidos cobre CA08 pela porta real (ligada como no
// main): só os pagos vão para estorno, com os ingressos cancelados.
func TestCancelarSessao_ComPedidos(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	sessaoID := novaSessao(t)
	abrir := func(assento string) pedidoDTO {
		return criadoDe(t, a.criar(a.carrinho(t, sessaoID, assento), sessaoID, assento)).Pedido
	}
	pago1, pago2, aguardando, expirado, estornado := abrir("A1"), abrir("A2"), abrir("A3"), abrir("A4"), abrir("A5")
	a.pagar(t, pago1.ID)
	a.pagar(t, pago2.ID)
	for id, st := range map[uuid.UUID]Status{expirado.ID: Expirado, estornado.ID: Estornado} {
		if _, err := pool.Exec(ctx, `UPDATE pedidos SET status = $2 WHERE id = $1`, id, string(st)); err != nil {
			t.Fatal(err)
		}
	}

	n, err := a.sessoes.CancelarSessao(ctx, sessaoID, uuid.NewString())
	if err != nil || n != 2 {
		t.Fatalf("cancelar sessão: n=%d err=%v", n, err)
	}
	for _, p := range []pedidoDTO{pago1, pago2} {
		if e := efeitosDe(t, p.ID); e.status != string(EstornoPendente) || e.motivo != MotivoSessaoCancelada || e.ingressos != 0 {
			t.Fatalf("pago %s: %+v", p.ID, e)
		}
	}
	for id, st := range map[uuid.UUID]Status{aguardando.ID: AguardandoPagamento, expirado.ID: Expirado, estornado.ID: Estornado} {
		if got := statusDe(t, id); got != string(st) {
			t.Fatalf("%s deveria continuar %s: %s", id, st, got)
		}
	}
	if lerContador(a.cancel, OrigemSessao) != 2 {
		t.Fatalf("cancelamentos_total{sessao}: %d", lerContador(a.cancel, OrigemSessao))
	}
	if n, err := a.sessoes.CancelarSessao(ctx, sessaoID, uuid.NewString()); err != nil || n != 0 {
		t.Fatalf("repetir: n=%d err=%v", n, err)
	}
}

// TestPivo_SessaoCancelada cobre CA09: pagamento confirmado depois do
// cancelamento da sessão → estorno, sem ingresso.
func TestPivo_SessaoCancelada(t *testing.T) {
	a := novoAmbiente(t)
	p, _, sessaoID := a.pedidoCom(t, "B1")
	if _, err := a.sessoes.CancelarSessao(context.Background(), sessaoID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if r := a.webhook(aprovado(p)); r.code != http.StatusOK {
		t.Fatalf("webhook: %d %s", r.code, r.corpo)
	}
	if e := efeitosDe(t, p.ID); e.status != string(EstornoPendente) || e.motivo != MotivoSessaoCancelada || e.ingressos != 0 || e.convertidos != 0 {
		t.Fatalf("efeitos: %+v", e)
	}
}

// TestCancelarSessao_CorridaComPivo: webhook e cancelamento da sessão ao
// mesmo tempo terminam sempre no mesmo lugar — estorno por sessão cancelada,
// nenhum ingresso ativo (ordem de travas do RF11/RF12).
func TestCancelarSessao_CorridaComPivo(t *testing.T) {
	a := novoAmbiente(t)
	for range 5 {
		p, _, sessaoID := a.pedidoCom(t, "C1")
		var wg sync.WaitGroup
		inicio := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-inicio
			a.webhook(aprovado(p))
		}()
		go func() {
			defer wg.Done()
			<-inicio
			if _, err := a.sessoes.CancelarSessao(context.Background(), sessaoID, uuid.NewString()); err != nil {
				t.Errorf("cancelar sessão: %v", err)
			}
		}()
		close(inicio)
		wg.Wait()
		if e := efeitosDe(t, p.ID); e.status != string(EstornoPendente) || e.motivo != MotivoSessaoCancelada || e.ingressos != 0 {
			t.Fatalf("corrida terminou inconsistente: %+v", e)
		}
	}
}
