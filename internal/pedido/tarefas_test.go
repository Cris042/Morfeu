//go:build integration
// +build integration

package pedido

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tarefas do worker (PRD 0025): reconciliação e estornos com o gateway fake
// programável, PG real e relógio injetado — gatilho explícito, sem ticker.

// semPendenciasAlheias: as tarefas varrem a tabela inteira; os testes do
// pacote rodam em sequência, então cada teste de tarefas começa encerrando as
// pendências deixadas por outros testes (cujas cobranças não existem no fake
// deste ambiente).
func semPendenciasAlheias(t *testing.T) {
	t.Helper()
	for _, sql := range []string{
		`UPDATE pedidos SET status = 'estornado' WHERE status = 'estorno_pendente'`,
		`UPDATE pedidos SET status = 'expirado' WHERE status = 'aguardando_pagamento'`,
	} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
}

func intencaoDe(p pedidoDTO) string { return "pi_fake_" + p.ID.String() }

func (a *ambiente) ocupados(t *testing.T, sessaoID int64) string {
	t.Helper()
	o, err := a.reserva.Ocupacao(context.Background(), sessaoID)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(o.Ocupados, ",")
}

// TestEstorno_Executa cobre CA01: estorno com a chave do pedido, estornado,
// assentos devolvidos, idempotente numa 2ª rodada.
func TestEstorno_Executa(t *testing.T) {
	semPendenciasAlheias(t)
	a := novoAmbiente(t)
	p, _, sessaoID := a.pedidoCom(t, "A1", "A2")
	ev := aprovado(p)
	ev.valor = 1 // divergência: pedido ainda aguardando, holds presos
	_ = a.webhook(ev)
	if a.ocupados(t, sessaoID) != "A1,A2" {
		t.Fatal("pré-condição: holds presos")
	}
	if r := a.servico.ExecutarEstornos(context.Background()); r.Estornados != 1 || r.Falhas != 0 {
		t.Fatalf("rodada: %+v", r)
	}
	if e := efeitosDe(t, p.ID); e.status != "estornado" || e.motivo != MotivoDivergencia {
		t.Fatalf("efeitos: %+v", e)
	}
	if got := a.gateway.Estornos(); len(got) != 1 || got[0] != ChaveEstorno(p.ID) {
		t.Fatalf("estornos no gateway: %v", got)
	}
	if got := trilha(t, p.ID); !strings.HasSuffix(got, "aguardando_pagamento>estorno_pendente estorno_pendente>estornado") {
		t.Fatalf("trilha: %s", got)
	}
	if o := a.ocupados(t, sessaoID); o != "" {
		t.Fatalf("assentos não voltaram: %s", o)
	}
	if r := a.servico.ExecutarEstornos(context.Background()); r.Estornados != 0 || len(a.gateway.Estornos()) != 1 {
		t.Fatalf("2ª rodada: %+v", r)
	}
	if a.etapa(EtapaEstornado) != 1 {
		t.Fatal("funil sem estornado")
	}
}

// TestEstorno_FalhaEBackoff cobre CA02: falha → tentativa registrada; nova
// rodada antes do backoff não chama o gateway; depois, a MESMA chave conclui.
func TestEstorno_FalhaEBackoff(t *testing.T) {
	semPendenciasAlheias(t)
	a := novoAmbiente(t)
	p, car, sessaoID := a.pedidoCom(t, "B1")
	a.rel.avancar(TTLPedido)
	_ = a.criar(car, sessaoID, "B1") // expira (lazy)
	_ = a.webhook(aprovado(p))       // pagamento tardio → estorno pendente
	a.gateway.FalharEstornos(1)
	if r := a.servico.ExecutarEstornos(context.Background()); r.Falhas != 1 {
		t.Fatalf("1ª: %+v", r)
	}
	var tentativas int
	_ = pool.QueryRow(context.Background(), `SELECT tentativas_estorno FROM pedidos WHERE id = $1`, p.ID).Scan(&tentativas)
	if tentativas != 1 || efeitosDe(t, p.ID).status != "estorno_pendente" {
		t.Fatalf("tentativas=%d", tentativas)
	}
	if r := a.servico.ExecutarEstornos(context.Background()); r.Estornados != 0 || len(a.gateway.Estornos()) != 1 {
		t.Fatalf("antes do backoff: %+v", r)
	}
	a.rel.avancar(backoffEstorno(1))
	if r := a.servico.ExecutarEstornos(context.Background()); r.Estornados != 1 {
		t.Fatalf("depois do backoff: %+v", r)
	}
	if got := a.gateway.Estornos(); len(got) != 2 || got[0] != got[1] || got[1] != ChaveEstorno(p.ID) {
		t.Fatalf("chaves: %v", got)
	}
}

func TestBackoffEstorno(t *testing.T) {
	for n, quer := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 7: time.Hour, 50: time.Hour} {
		if got := backoffEstorno(n); got != quer {
			t.Errorf("%d: %v", n, got)
		}
	}
}

// TestReconciliar_ExpiraECancela cobre CA03: vencido sem pagamento →
// cobrança cancelada, pedido expirado, assentos devolvidos; no prazo, intocado.
func TestReconciliar_ExpiraECancela(t *testing.T) {
	semPendenciasAlheias(t)
	a := novoAmbiente(t)
	p, _, sessaoID := a.pedidoCom(t, "C1")
	if r := a.servico.Reconciliar(context.Background()); r != (ResumoTarefas{}) {
		t.Fatalf("no prazo: %+v", r)
	}
	a.rel.avancar(TTLPedido)
	if r := a.servico.Reconciliar(context.Background()); r.Expirados != 1 {
		t.Fatalf("vencido: %+v", r)
	}
	if s := statusDe(t, p.ID); s != "expirado" || !slices.Contains(a.gateway.Cancelamentos(), intencaoDe(p)) {
		t.Fatalf("status %s, cancelamentos %v", s, a.gateway.Cancelamentos())
	}
	if o := a.ocupados(t, sessaoID); o != "" {
		t.Fatalf("assentos: %s", o)
	}
}

// TestReconciliar_WebhookPerdido cobre CA04: cobrança aprovada sem webhook →
// o mesmo pivô emite os ingressos.
func TestReconciliar_WebhookPerdido(t *testing.T) {
	semPendenciasAlheias(t)
	a := novoAmbiente(t)
	p, _, _ := a.pedidoCom(t, "C2", "C3")
	a.gateway.Aprovar(intencaoDe(p))
	a.rel.avancar(TTLPedido)
	if r := a.servico.Reconciliar(context.Background()); r.Confirmados != 1 {
		t.Fatalf("rodada: %+v", r)
	}
	if e := efeitosDe(t, p.ID); e != (efeitos{status: "pago", ingressos: 2, outbox: 1, convertidos: 2}) {
		t.Fatalf("efeitos: %+v", e)
	}
	// O webhook atrasado chega depois: sem efeito.
	if r := a.webhook(aprovado(p)); r.code != http.StatusOK || efeitosDe(t, p.ID).outbox != 1 {
		t.Fatalf("webhook atrasado: %d", r.code)
	}
}

// TestReconciliar_AssentoPerdido cobre CA05: aprovado tarde demais, com
// assento levado por outro carrinho → estorno automático completo.
func TestReconciliar_AssentoPerdido(t *testing.T) {
	semPendenciasAlheias(t)
	a := novoAmbiente(t)
	p, _, sessaoID := a.pedidoCom(t, "A5")
	a.gateway.Aprovar(intencaoDe(p))
	a.rel.avancar(TTLPedido + MargemHold)
	if _, err := a.reserva.Travar(context.Background(), sessaoID, []string{"A5"}, donoNovo(t)); err != nil {
		t.Fatal(err)
	}
	_ = a.servico.Reconciliar(context.Background())
	if e := efeitosDe(t, p.ID); e != (efeitos{status: "estorno_pendente", motivo: MotivoEmissao}) {
		t.Fatalf("efeitos: %+v", e)
	}
	_ = a.servico.ExecutarEstornos(context.Background())
	if s := statusDe(t, p.ID); s != "estornado" {
		t.Fatalf("status: %s", s)
	}
}

// TestReconciliar_CorridaComWebhook cobre CA06: reconciliação e webhook do
// mesmo pagamento ao mesmo tempo → um único efeito.
func TestReconciliar_CorridaComWebhook(t *testing.T) {
	semPendenciasAlheias(t)
	a := novoAmbiente(t)
	for rodada := 1; rodada <= 5; rodada++ {
		p, _, _ := a.pedidoCom(t, "B7")
		a.gateway.Aprovar(intencaoDe(p))
		a.rel.avancar(TTLPedido)
		largada := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-largada; _ = a.servico.Reconciliar(context.Background()) }()
		go func() { defer wg.Done(); <-largada; _ = a.webhook(aprovado(p)) }()
		close(largada)
		wg.Wait()
		if e := efeitosDe(t, p.ID); e != (efeitos{status: "pago", ingressos: 1, outbox: 1, convertidos: 1}) {
			t.Fatalf("rodada %d: %+v", rodada, e)
		}
	}
}

// TestCriar_ExpiracaoLazyCancelaCobranca cobre CA07: a expiração lazy na
// criação também cancela a cobrança do pedido vencido.
func TestCriar_ExpiracaoLazyCancelaCobranca(t *testing.T) {
	a := novoAmbiente(t)
	p, car, sessaoID := a.pedidoCom(t, "C5")
	a.rel.avancar(TTLPedido)
	_ = a.criar(car, sessaoID, "C5")
	if !slices.Contains(a.gateway.Cancelamentos(), intencaoDe(p)) {
		t.Fatalf("cobrança do vencido não cancelada: %v", a.gateway.Cancelamentos())
	}
}

// TestReconciliar_Adiamentos cobre o RF02 (auditoria 0025): consulta que
// falha adia o pedido; pedido sem cobrança expira direto.
func TestReconciliar_Adiamentos(t *testing.T) {
	semPendenciasAlheias(t)
	a := novoAmbiente(t)
	p, _, _ := a.pedidoCom(t, "A7")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE pedidos SET payment_intent_id = 'pi_desconhecido' WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	a.rel.avancar(TTLPedido)
	if r := a.servico.Reconciliar(ctx); r.Falhas != 1 || statusDe(t, p.ID) != "aguardando_pagamento" {
		t.Fatalf("consulta falhou: %+v %s", r, statusDe(t, p.ID))
	}
	if _, err := pool.Exec(ctx, `UPDATE pedidos SET payment_intent_id = NULL WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if r := a.servico.Reconciliar(ctx); r.Expirados != 1 || statusDe(t, p.ID) != "expirado" {
		t.Fatalf("sem cobrança: %+v %s", r, statusDe(t, p.ID))
	}
	cancelado, fim := context.WithCancel(ctx)
	fim()
	if r := a.servico.Reconciliar(cancelado); r.Expirados+r.Confirmados != 0 {
		t.Fatalf("com o contexto encerrado nada é processado: %+v", r)
	}
}
