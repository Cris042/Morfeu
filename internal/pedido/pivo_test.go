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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stripe/stripe-go/v86/webhook"

	"github.com/mclovin137/morfeu/internal/reserva"
)

// Webhook + pivô (PRD 0024) pelas rotas reais, sem rede: o evento é montado
// e assinado no próprio teste com o segredo da suíte. A tolerância de
// timestamp usa o relógio real (a lib do Stripe), então a assinatura usa
// time.Now — o relógio injetado do domínio segue em 2098.

type eventoTeste struct {
	id     string
	tipo   string
	pedido string
	pi     string
	valor  int64
	moeda  string
}

func (e eventoTeste) corpo() []byte {
	b, _ := json.Marshal(map[string]any{
		"id": e.id, "object": "event", "type": e.tipo, "api_version": "2020-01-01",
		"data": map[string]any{"object": map[string]any{
			"id": e.pi, "object": "payment_intent", "amount": e.valor, "currency": e.moeda,
			"metadata": map[string]string{"pedido_id": e.pedido},
		}},
	})
	return b
}

// aprovado monta o payment_intent.succeeded do pedido como o Stripe mandaria.
func aprovado(p pedidoDTO) eventoTeste {
	return eventoTeste{id: "evt_" + uuid.NewString(), tipo: "payment_intent.succeeded", pedido: p.ID.String(),
		pi: "pi_fake_" + p.ID.String(), valor: p.TotalCentavos, moeda: "brl"}
}

func (a *ambiente) webhookBruto(corpo []byte, assinatura string) resposta {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(corpo))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if assinatura != "" {
		r.Header.Set(cabecalhoAssinatura, assinatura)
	}
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, r)
	return resposta{code: rec.Code, corpo: strings.TrimSpace(rec.Body.String()), cabecalho: rec.Header()}
}

func assinar(corpo []byte) string {
	return webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: corpo, Secret: segredoWebhookTeste, Timestamp: time.Now()}).Header
}

func (a *ambiente) webhook(e eventoTeste) resposta {
	c := e.corpo()
	return a.webhookBruto(c, assinar(c))
}

type efeitos struct {
	status, motivo string
	ingressos      int
	outbox         int
	convertidos    int
}

func efeitosDe(t *testing.T, id uuid.UUID) efeitos {
	t.Helper()
	ctx := context.Background()
	var e efeitos
	var motivo *string
	if err := pool.QueryRow(ctx, `SELECT status, motivo_estorno FROM pedidos WHERE id = $1`, id).Scan(&e.status, &motivo); err != nil {
		t.Fatal(err)
	}
	if motivo != nil {
		e.motivo = *motivo
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM ingressos WHERE pedido_id = $1 AND status = 'ativo'`, id).Scan(&e.ingressos)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = $2`, id.String(), EventoConfirmado).Scan(&e.outbox)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM holds WHERE pedido_id = $1 AND status = 'convertido'`, id).Scan(&e.convertidos)
	return e
}

func (a *ambiente) pedidoCom(t *testing.T, assentos ...string) (pedidoDTO, string, int64) {
	t.Helper()
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID, assentos...)
	return criadoDe(t, a.criar(car, sessaoID, assentos...)).Pedido, car, sessaoID
}

// TestWebhook_Pivo cobre CA01 e CA02: pago, ingressos, holds vendidos,
// evento na outbox só com o id, trilha e ocupação.
func TestWebhook_Pivo(t *testing.T) {
	a := novoAmbiente(t)
	p, _, sessaoID := a.pedidoCom(t, "A1", "A2")
	if r := a.webhook(aprovado(p)); r.code != http.StatusOK {
		t.Fatalf("webhook: %d %s", r.code, r.corpo)
	}
	if e := efeitosDe(t, p.ID); e != (efeitos{status: "pago", ingressos: 2, outbox: 1, convertidos: 2}) {
		t.Fatalf("efeitos: %+v", e)
	}
	var payload string
	_ = pool.QueryRow(context.Background(), `SELECT payload::text FROM outbox_events WHERE aggregate_id = $1`, p.ID.String()).Scan(&payload)
	if payload != `{"pedido_id": "`+p.ID.String()+`"}` {
		t.Fatalf("payload do evento: %s", payload)
	}
	if got := trilha(t, p.ID); got != "->aguardando_pagamento aguardando_pagamento>pago" {
		t.Fatalf("trilha: %s", got)
	}
	if a.etapa(EtapaPago) != 1 {
		t.Fatal("funil sem pago")
	}
	o, err := a.reserva.Ocupacao(context.Background(), sessaoID)
	if err != nil || strings.Join(o.Ocupados, ",") != "A1,A2" {
		t.Fatalf("ocupação: %+v %v", o, err)
	}
	a.rel.avancar(24 * time.Hour)
	if _, err := a.reserva.Travar(context.Background(), sessaoID, []string{"A1"}, donoNovo(t)); err == nil {
		t.Fatal("assento vendido voltou a ser travável")
	}
}

func donoNovo(t *testing.T) reserva.Dono {
	t.Helper()
	_, d, err := reserva.NovoCarrinho()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestWebhook_Duplicado cobre CA03: 10 entregas simultâneas do mesmo evento
// → todas 2xx e um único efeito.
func TestWebhook_Duplicado(t *testing.T) {
	a := novoAmbiente(t)
	p, _, _ := a.pedidoCom(t, "B1")
	ev := aprovado(p)
	corpo := ev.corpo()
	assinatura := assinar(corpo)
	largada := make(chan struct{})
	var wg sync.WaitGroup
	rs := make([]resposta, 10)
	for i := range rs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-largada
			rs[i] = a.webhookBruto(corpo, assinatura)
		}(i)
	}
	close(largada)
	wg.Wait()
	for i, r := range rs {
		if r.code != http.StatusOK {
			t.Fatalf("entrega %d: %d %s", i, r.code, r.corpo)
		}
	}
	if e := efeitosDe(t, p.ID); e != (efeitos{status: "pago", ingressos: 1, outbox: 1, convertidos: 1}) {
		t.Fatalf("efeitos: %+v", e)
	}
	// Evento novo para o mesmo pedido já pago: sem efeito.
	outro := ev
	outro.id = "evt_" + uuid.NewString()
	if r := a.webhook(outro); r.code != http.StatusOK || efeitosDe(t, p.ID).outbox != 1 {
		t.Fatalf("2º evento: %d", r.code)
	}
}

// TestWebhook_Assinatura cobre CA04: sem efeito e sem detalhe na resposta.
func TestWebhook_Assinatura(t *testing.T) {
	a := novoAmbiente(t)
	p, _, _ := a.pedidoCom(t, "C1")
	corpo := aprovado(p).corpo()
	adulterado := bytes.Replace(corpo, []byte(`"amount":3000`), []byte(`"amount":3001`), 1)
	outroSegredo := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: corpo, Secret: "whsec_outro", Timestamp: time.Now()}).Header
	antigo := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: corpo, Secret: segredoWebhookTeste, Timestamp: time.Now().Add(-10 * time.Minute)}).Header
	for nome, r := range map[string]resposta{
		"sem assinatura":   a.webhookBruto(corpo, ""),
		"corpo adulterado": a.webhookBruto(adulterado, assinar(corpo)),
		"outro segredo":    a.webhookBruto(corpo, outroSegredo),
		"timestamp antigo": a.webhookBruto(corpo, antigo),
	} {
		if r.code != http.StatusBadRequest || r.corpo != `{"erro":"assinatura_invalida"}` {
			t.Errorf("%s: %d %s", nome, r.code, r.corpo)
		}
	}
	grande := append(bytes.Repeat([]byte(" "), 70*1024), corpo...)
	if r := a.webhookBruto(grande, assinar(grande)); r.code != http.StatusRequestEntityTooLarge {
		t.Errorf("corpo > 64 KB: %d", r.code)
	}
	if e := efeitosDe(t, p.ID); e.status != "aguardando_pagamento" || e.ingressos != 0 {
		t.Fatalf("efeito com assinatura inválida: %+v", e)
	}
}

// TestWebhook_Estornos cobre CA05–CA07: divergência, pagamento tardio e
// assento perdido → estorno_pendente com o motivo, sem venda parcial.
func TestWebhook_Estornos(t *testing.T) {
	a := novoAmbiente(t)

	t.Run("valor divergente", func(t *testing.T) {
		p, _, _ := a.pedidoCom(t, "A1")
		ev := aprovado(p)
		ev.valor = 1
		if r := a.webhook(ev); r.code != http.StatusOK {
			t.Fatalf("%d %s", r.code, r.corpo)
		}
		if e := efeitosDe(t, p.ID); e != (efeitos{status: "estorno_pendente", motivo: MotivoDivergencia}) {
			t.Fatalf("%+v", e)
		}
	})
	t.Run("moeda divergente", func(t *testing.T) {
		p, _, _ := a.pedidoCom(t, "A1")
		ev := aprovado(p)
		ev.moeda = "usd"
		_ = a.webhook(ev)
		if e := efeitosDe(t, p.ID); e.status != "estorno_pendente" || e.motivo != MotivoDivergencia {
			t.Fatalf("%+v", e)
		}
	})
	t.Run("pagamento tardio", func(t *testing.T) {
		p, car, sessaoID := a.pedidoCom(t, "A1")
		a.rel.avancar(TTLPedido)
		_ = a.criar(car, sessaoID, "A1") // expiração lazy do pendente
		if s := statusDe(t, p.ID); s != "expirado" {
			t.Fatalf("pré-condição: %s", s)
		}
		_ = a.webhook(aprovado(p))
		if e := efeitosDe(t, p.ID); e != (efeitos{status: "estorno_pendente", motivo: MotivoTardio}) {
			t.Fatalf("%+v", e)
		}
		if got := trilha(t, p.ID); !strings.HasSuffix(got, "expirado>estorno_pendente") {
			t.Fatalf("trilha: %s", got)
		}
	})
	t.Run("assento perdido", func(t *testing.T) {
		p, _, sessaoID := a.pedidoCom(t, "A1", "A2")
		a.rel.avancar(TTLPedido + MargemHold) // o hold preso venceu…
		ladrao := donoNovo(t)
		if _, err := a.reserva.Travar(context.Background(), sessaoID, []string{"A2"}, ladrao); err != nil {
			t.Fatalf("roubo: %v", err) // …e outro carrinho levou A2
		}
		_ = a.webhook(aprovado(p))
		if e := efeitosDe(t, p.ID); e != (efeitos{status: "estorno_pendente", motivo: MotivoEmissao}) {
			t.Fatalf("venda parcial ou estado errado: %+v", e)
		}
		if n, _ := contarHoldsAtivos(t, sessaoID, "A2", ladrao); n != 1 {
			t.Fatal("o hold do outro carrinho foi afetado")
		}
	})
	if a.etapa(EtapaEstornoNecessario) != 4 {
		t.Fatalf("funil estorno_necessario = %d", a.etapa(EtapaEstornoNecessario))
	}
}

func contarHoldsAtivos(t *testing.T, sessaoID int64, assento string, d reserva.Dono) (int, error) {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `SELECT count(*) FROM holds WHERE sessao_id = $1 AND assento_codigo = $2 AND status = 'ativo' AND dono_hash = $3`,
		sessaoID, assento, d.Hash()).Scan(&n)
	return n, err
}

// TestWebhook_CorridaComExpiracao cobre CA08: webhook × expiração lazy do
// mesmo pedido, em paralelo → nunca ingresso sem pagamento válido, nunca
// pagamento sem ingresso ou estorno.
func TestWebhook_CorridaComExpiracao(t *testing.T) {
	a := novoAmbiente(t)
	for rodada := 1; rodada <= 10; rodada++ {
		p, car, sessaoID := a.pedidoCom(t, "B5")
		a.rel.avancar(TTLPedido)
		largada := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-largada; _ = a.criar(car, sessaoID, "B5") }()
		go func() { defer wg.Done(); <-largada; _ = a.webhook(aprovado(p)) }()
		close(largada)
		wg.Wait()
		e := efeitosDe(t, p.ID)
		pago := e == (efeitos{status: "pago", ingressos: 1, outbox: 1, convertidos: 1})
		tardio := e == (efeitos{status: "estorno_pendente", motivo: MotivoTardio})
		if !pago && !tardio {
			t.Fatalf("rodada %d: estado inválido %+v", rodada, e)
		}
	}
}

// TestWebhook_SemEfeito cobre CA09: recusado, desconhecido, pedido
// inexistente e cobrança de outro pedido → 2xx sem mudança.
func TestWebhook_SemEfeito(t *testing.T) {
	a := novoAmbiente(t)
	p, _, _ := a.pedidoCom(t, "C3")
	recusado := aprovado(p)
	recusado.tipo = "payment_intent.payment_failed"
	desconhecido := aprovado(p)
	desconhecido.tipo = "charge.refunded"
	semPedido := aprovado(p)
	semPedido.pedido = uuid.NewString()
	semMetadata := aprovado(p)
	semMetadata.pedido = ""
	outraCobranca := aprovado(p)
	outraCobranca.pi = "pi_outro"
	for nome, ev := range map[string]eventoTeste{
		"recusado": recusado, "desconhecido": desconhecido, "pedido inexistente": semPedido,
		"sem metadata": semMetadata, "outra cobrança": outraCobranca,
	} {
		if r := a.webhook(ev); r.code != http.StatusOK {
			t.Errorf("%s: %d %s", nome, r.code, r.corpo)
		}
	}
	if e := efeitosDe(t, p.ID); e != (efeitos{status: "aguardando_pagamento"}) {
		t.Fatalf("efeito indevido: %+v", e)
	}
	if a.etapa(EtapaPagamentoRecusado) != 1 {
		t.Fatal("funil sem pagamento_recusado")
	}
}

// TestWebhook_CobrancaOrfa cobre CA10 (auditoria 0023): o gateway criou a
// cobrança mas o pedido não a gravou — o pivô a recupera pelo metadata.
func TestWebhook_CobrancaOrfa(t *testing.T) {
	a := novoAmbiente(t)
	p, _, _ := a.pedidoCom(t, "A9")
	if _, err := pool.Exec(context.Background(), `UPDATE pedidos SET payment_intent_id = NULL WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	_ = a.webhook(aprovado(p))
	var pi string
	_ = pool.QueryRow(context.Background(), `SELECT payment_intent_id FROM pedidos WHERE id = $1`, p.ID).Scan(&pi)
	if e := efeitosDe(t, p.ID); e.status != "pago" || pi != "pi_fake_"+p.ID.String() {
		t.Fatalf("órfã: %+v pi=%s", e, pi)
	}
}
