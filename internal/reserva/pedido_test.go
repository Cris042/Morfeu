//go:build integration
// +build integration

package reserva

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mclovin137/morfeu/internal/outbox"
)

// Portas transacionais do pedido (PRD 0022): mesma infraestrutura da suíte
// do E4 (PG + Redis reais, relógio injetado), TX real via outbox.WithTx —
// como o service do pedido fará na task 0023.

// prazoPedido é o prazo típico do hold preso: expira_em do pedido (15 min)
// + 2 min de margem (ADR 0010).
const prazoPedido = 17 * time.Minute

func donoTeste(t *testing.T, token string) Dono {
	t.Helper()
	d, ok := DonoDoToken(token)
	if !ok {
		t.Fatal("token inválido")
	}
	return d
}

func (a *ambiente) prender(d Dono, sessaoID int64, pedido uuid.UUID, ate time.Time, codigos ...string) error {
	return outbox.WithTx(context.Background(), pool, func(tx outbox.Tx) error {
		return a.servico.PrenderParaPedido(context.Background(), tx, d, sessaoID, codigos, pedido, ate)
	})
}

func (a *ambiente) converter(t *testing.T, pedido uuid.UUID) []string {
	t.Helper()
	var out []string
	err := outbox.WithTx(context.Background(), pool, func(tx outbox.Tx) error {
		var err error
		out, err = a.servico.ConverterDoPedido(context.Background(), tx, pedido)
		return err
	})
	if err != nil {
		t.Fatalf("converter: %v", err)
	}
	return out
}

func (a *ambiente) liberarDoPedido(t *testing.T, pedido uuid.UUID) int64 {
	t.Helper()
	var n int64
	err := outbox.WithTx(context.Background(), pool, func(tx outbox.Tx) error {
		var err error
		n, err = a.servico.LiberarDoPedido(context.Background(), tx, pedido)
		return err
	})
	if err != nil {
		t.Fatalf("liberar do pedido: %v", err)
	}
	return n
}

type linhaHold struct {
	id     uuid.UUID
	dono   []byte
	status string
	pedido *uuid.UUID
}

func linhasDoAssento(t *testing.T, sessaoID int64, assento string) []linhaHold {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT id, dono_hash, status, pedido_id FROM holds
		WHERE sessao_id = $1 AND assento_codigo = $2 AND status IN ('ativo', 'convertido') ORDER BY criado_em`, sessaoID, assento)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []linhaHold
	for rows.Next() {
		var l linhaHold
		if err := rows.Scan(&l.id, &l.dono, &l.status, &l.pedido); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

// vendidoIntacto confere que a linha do vendido é a mesma (id, dono, pedido).
func vendidoIntacto(t *testing.T, antes linhaHold, depois []linhaHold, d Dono, pedido uuid.UUID) {
	t.Helper()
	if len(depois) != 1 {
		t.Fatalf("linhas ocupando o assento vendido: %+v", depois)
	}
	l := depois[0]
	if l.id != antes.id || string(l.dono) != string(d.hash) || l.status != "convertido" || l.pedido == nil || *l.pedido != pedido {
		t.Fatalf("linha do vendido alterada: antes=%+v depois=%+v", antes, l)
	}
}

// TestPedido_ConvertidoOcupaParaSempre cobre CA01, CA04, CA07 e CA08.
func TestPedido_ConvertidoOcupaParaSempre(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	tok := novoDono(t)
	d := donoTeste(t, tok)
	if r := a.travar(sessaoID, tok, "A1", "A2"); r.code != http.StatusCreated {
		t.Fatalf("trava: %d %s", r.code, r.corpo)
	}
	pedido := uuid.New()
	if err := a.prender(d, sessaoID, pedido, a.rel.agora().Add(prazoPedido), "A1", "A2"); err != nil {
		t.Fatalf("prender: %v", err)
	}
	if got := a.converter(t, pedido); strings.Join(got, ",") != "A1,A2" {
		t.Fatalf("convertidos: %v", got)
	}
	antes := linhasDoAssento(t, sessaoID, "A1")

	// CA04: idempotente; a métrica conta só a conversão real.
	if got := a.converter(t, pedido); strings.Join(got, ",") != "A1,A2" {
		t.Fatalf("2ª conversão: %v", got)
	}
	if n := a.convertidos.Load(); n != 2 {
		t.Fatalf("métrica de convertidos = %d, quer 2", n)
	}

	// CA01: muito depois do prazo, ninguém rouba o vendido — nem sob corrida.
	a.rel.avancar(24 * time.Hour)
	if r := a.travar(sessaoID, novoDono(t), "A1"); r.code != http.StatusConflict || !strings.Contains(r.corpo, "assento_indisponivel") {
		t.Fatalf("trava sobre vendido: %d %s", r.code, r.corpo)
	}
	for i, r := range a.disputar(sessaoID, novosDonos(t, 20), "A2") {
		if r.code != http.StatusConflict {
			t.Fatalf("disputa %d sobre vendido: %d %s", i, r.code, r.corpo)
		}
	}
	vendidoIntacto(t, antes[0], linhasDoAssento(t, sessaoID, "A1"), d, pedido)

	// CA08: o sweeper não toca o vendido.
	if _, err := a.sweeper.VarrerExpirados(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l := linhasDoAssento(t, sessaoID, "A2"); len(l) != 1 || l[0].status != "convertido" {
		t.Fatalf("sweeper alterou o vendido: %+v", l)
	}

	// CA07: a ocupação mostra o vendido mesmo com o relógio muito adiante.
	expirarCache(t, sessaoID)
	if _, ocupados := a.ocupacao(t, sessaoID); strings.Join(ocupados, ",") != "A1,A2" {
		t.Fatalf("ocupação sem os vendidos: %v", ocupados)
	}
}

// TestPedido_HoldPresoNaoRoubavel cobre CA02, CA04 (roubo parcial) e CA06.
func TestPedido_HoldPresoNaoRoubavel(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	tok := novoDono(t)
	d := donoTeste(t, tok)
	holds := holdsDe(t, a.travar(sessaoID, tok, "B1", "B2"))
	pedido := uuid.New()
	inicio := a.rel.agora()
	if err := a.prender(d, sessaoID, pedido, inicio.Add(prazoPedido), "B1", "B2"); err != nil {
		t.Fatalf("prender: %v", err)
	}

	// Depois do TTL normal do hold (10 min) ele segue preso.
	a.rel.avancar(12 * time.Minute)
	if r := a.travar(sessaoID, novoDono(t), "B1"); r.code != http.StatusConflict {
		t.Fatalf("roubo de hold preso: %d %s", r.code, r.corpo)
	}
	if _, err := a.sweeper.VarrerExpirados(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l := linhasDoAssento(t, sessaoID, "B1"); len(l) != 1 || l[0].status != "ativo" {
		t.Fatalf("sweeper varreu hold preso: %+v", l)
	}

	// CA06: o cliente não estende nem libera o hold preso; ele segue listado.
	id := holds[0].ID.String()
	for nome, r := range map[string]resposta{
		"estender": a.req(http.MethodPost, "/holds/"+id+"/estender", tok, nil, true),
		"liberar":  a.req(http.MethodDelete, "/holds/"+id, tok, nil, true),
	} {
		if r.code != http.StatusConflict || !strings.Contains(r.corpo, "hold_em_pedido") {
			t.Fatalf("%s em hold preso: %d %s", nome, r.code, r.corpo)
		}
	}
	var meus []holdDTO
	if r := a.req(http.MethodGet, "/holds", tok, nil, false); json.Unmarshal([]byte(r.corpo), &meus) != nil || len(meus) != 2 {
		t.Fatalf("meus holds: %s", r.corpo)
	}

	// CA02: no fim do prazo do pedido o hold volta a ser roubável e perde o pedido.
	a.rel.avancar(inicio.Add(prazoPedido).Sub(a.rel.agora()))
	ladrao := novoDono(t)
	if r := a.travar(sessaoID, ladrao, "B1"); r.code != http.StatusCreated {
		t.Fatalf("roubo após o prazo: %d %s", r.code, r.corpo)
	}
	if l := linhasDoAssento(t, sessaoID, "B1"); len(l) != 1 || l[0].pedido != nil || string(l[0].dono) != string(donoTeste(t, ladrao).hash) {
		t.Fatalf("roubo não zerou o pedido: %+v", l)
	}

	// CA04: pagamento tardio converte só o que sobrou — quem chama estorna.
	if got := a.converter(t, pedido); strings.Join(got, ",") != "B2" {
		t.Fatalf("conversão parcial: %v", got)
	}
}

// TestPedido_PrenderValidaCobertura cobre CA03.
func TestPedido_PrenderValidaCobertura(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	tokA, tokB := novoDono(t), novoDono(t)
	dA := donoTeste(t, tokA)
	_ = a.travar(sessaoID, tokA, "C1", "C2")
	_ = a.travar(sessaoID, tokB, "C3")
	ate := a.rel.agora().Add(prazoPedido)
	pedido := uuid.New()

	casos := map[string][]string{
		"de outro dono": {"C1", "C3"},
		"sem hold":      {"C1", "C4"},
	}
	for nome, codigos := range casos {
		if err := a.prender(dA, sessaoID, pedido, ate, codigos...); !errors.Is(err, ErrHoldsDoPedido) {
			t.Errorf("%s: err = %v", nome, err)
		}
	}
	if err := a.prender(dA, novaSessao(t), pedido, ate, "C1"); !errors.Is(err, ErrHoldsDoPedido) {
		t.Errorf("sessão diferente: err = %v", err)
	}
	if err := a.prender(dA, sessaoID, pedido, ate, "C1", "C1"); !errors.Is(err, ErrDadosInvalidos) {
		t.Errorf("lote repetido: err = %v", err)
	}
	if err := a.prender(dA, sessaoID, pedido, a.rel.agora(), "C1"); !errors.Is(err, ErrDadosInvalidos) {
		t.Errorf("prazo no passado: err = %v", err)
	}
	// A TX desfeita não deixou nenhum hold preso.
	if l := linhasDoAssento(t, sessaoID, "C1"); l[0].pedido != nil {
		t.Fatalf("falha parcial deixou C1 preso: %+v", l)
	}

	// Idempotente para o mesmo pedido; outro pedido não pega o hold já preso.
	for i := range 2 {
		if err := a.prender(dA, sessaoID, pedido, ate, "C1", "C2"); err != nil {
			t.Fatalf("prender %d: %v", i+1, err)
		}
	}
	if err := a.prender(dA, sessaoID, uuid.New(), ate, "C1"); !errors.Is(err, ErrHoldsDoPedido) {
		t.Errorf("outro pedido sobre hold preso: err = %v", err)
	}

	// Vencido não é preso.
	_ = a.travar(sessaoID, tokA, "C5")
	a.rel.avancar(TTLHold)
	if err := a.prender(dA, sessaoID, uuid.New(), a.rel.agora().Add(prazoPedido), "C5"); !errors.Is(err, ErrHoldsDoPedido) {
		t.Errorf("vencido: err = %v", err)
	}
}

// TestPedido_LiberarDoPedido cobre CA05.
func TestPedido_LiberarDoPedido(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	tok := novoDono(t)
	_ = a.travar(sessaoID, tok, "A5", "A6")
	pedido := uuid.New()
	if err := a.prender(donoTeste(t, tok), sessaoID, pedido, a.rel.agora().Add(prazoPedido), "A5", "A6"); err != nil {
		t.Fatal(err)
	}
	if n := a.liberarDoPedido(t, pedido); n != 2 {
		t.Fatalf("liberados = %d", n)
	}
	if n := a.liberarDoPedido(t, pedido); n != 0 {
		t.Fatalf("2ª liberação = %d", n)
	}
	outro := novoDono(t)
	if r := a.travar(sessaoID, outro, "A5", "A6"); r.code != http.StatusCreated {
		t.Fatalf("assento não voltou: %d %s", r.code, r.corpo)
	}
	// Vendido ocupa o assento até o pedido ser estornado; o estorno (único
	// chamador com holds vendidos — ADR 0011) devolve também os vendidos.
	pedido2 := uuid.New()
	if err := a.prender(donoTeste(t, outro), sessaoID, pedido2, a.rel.agora().Add(prazoPedido), "A5", "A6"); err != nil {
		t.Fatal(err)
	}
	a.converter(t, pedido2)
	expirarCache(t, sessaoID)
	if _, ocupados := a.ocupacao(t, sessaoID); !slices.Equal(ocupados, []string{"A5", "A6"}) {
		t.Fatalf("ocupação: %v", ocupados)
	}
	if n := a.liberarDoPedido(t, pedido2); n != 2 {
		t.Fatalf("estorno deveria devolver os vendidos: %d", n)
	}
	if r := a.travar(sessaoID, novoDono(t), "A5", "A6"); r.code != http.StatusCreated {
		t.Fatalf("vendido estornado não voltou: %d %s", r.code, r.corpo)
	}
}
