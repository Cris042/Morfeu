package pedido

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Matriz COMPLETA estado × evento (refinamento E6, QA), declarada aqui à
// parte da tabela do código: acrescentar estado ou evento sem
// atualizar esta matriz quebra o teste. "" = transição ilegal.
func TestTransicionar_MatrizCompleta(t *testing.T) {
	estados := []Status{AguardandoPagamento, Pago, Expirado, Falhou, EstornoPendente, Estornado}
	eventos := []Evento{PagamentoConfirmado, CobrancaFalhou, PrazoVencido, EstornoNecessario, EstornoConcluido}
	esperado := map[Status]map[Evento]Status{
		AguardandoPagamento: {
			PagamentoConfirmado: Pago,
			CobrancaFalhou:      Falhou,
			PrazoVencido:        Expirado,
			EstornoNecessario:   EstornoPendente,
		},
		Expirado:        {EstornoNecessario: EstornoPendente},
		EstornoPendente: {EstornoConcluido: Estornado},
	}
	if len(transicoes) > len(estados) {
		t.Fatalf("a tabela do código tem estados fora da matriz: %v", transicoes)
	}
	for _, de := range estados {
		for _, ev := range eventos {
			conferirCelula(t, de, ev, esperado[de][ev])
		}
	}
	// O pivô é irreversível: de pago nada sai no E6 (cancelamento é do E9).
	for _, ev := range eventos {
		if _, err := Transicionar(Pago, ev); err == nil {
			t.Errorf("pago + %s deveria ser ilegal", ev)
		}
	}
}

// conferirCelula confere uma célula da matriz ("" = transição ilegal).
func conferirCelula(t *testing.T, de Status, ev Evento, quer Status) {
	t.Helper()
	got, err := Transicionar(de, ev)
	if quer == "" {
		var et *ErroTransicao
		if !errors.As(err, &et) || !errors.Is(err, ErrTransicaoIlegal) || got != "" {
			t.Errorf("%s + %s: quer ilegal, veio %q (%v)", de, ev, got, err)
		}
		return
	}
	if err != nil || got != quer {
		t.Errorf("%s + %s: quer %s, veio %q (%v)", de, ev, quer, got, err)
	}
}

var agoraTeste = time.Date(2098, 1, 1, 12, 0, 0, 0, time.UTC)

func TestNovoPedido(t *testing.T) {
	hash := make([]byte, 32)
	p, err := NovoPedido(Entrada{Email: "ana@exemplo.com", SessaoID: 7, Assentos: []string{"B2", "A1"}}, hash, 3000, agoraTeste)
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalCentavos() != 6000 || p.Status() != AguardandoPagamento || !p.ExpiraEm().Equal(agoraTeste.Add(15*time.Minute)) ||
		!p.HoldAte().Equal(agoraTeste.Add(17*time.Minute)) || strings.Join(p.Assentos(), ",") != "B2,A1" {
		t.Fatalf("pedido: %+v", p)
	}
	if !regexp.MustCompile(`^[A-Z2-7]{16}$`).MatchString(p.Codigo()) {
		t.Fatalf("código: %q", p.Codigo())
	}
	outro, _ := NovoPedido(Entrada{Email: "ana@exemplo.com", SessaoID: 7, Assentos: []string{"A1"}}, hash, 3000, agoraTeste)
	if outro.Codigo() == p.Codigo() || outro.ID() == p.ID() {
		t.Fatal("código e id devem ser aleatórios")
	}
	if _, err := NovoPedido(Entrada{Email: "ana@exemplo.com", SessaoID: 7, Assentos: []string{"A1"}}, hash, 0, agoraTeste); err == nil {
		t.Fatal("preço zero deveria falhar")
	}
}

func TestEntrada_Validacao(t *testing.T) {
	ok := Entrada{Email: "ana@exemplo.com", SessaoID: 1, Assentos: []string{"A1"}}
	casos := map[string]struct {
		mudar func(*Entrada)
		campo string
	}{
		"email vazio":          {func(e *Entrada) { e.Email = "" }, "email"},
		"email sem arroba":     {func(e *Entrada) { e.Email = "ana.exemplo.com" }, "email"},
		"email com nome":       {func(e *Entrada) { e.Email = "Ana <ana@exemplo.com>" }, "email"},
		"email com espaço":     {func(e *Entrada) { e.Email = " ana@exemplo.com" }, "email"},
		"email longo":          {func(e *Entrada) { e.Email = strings.Repeat("a", 250) + "@x.com" }, "email"},
		"sessão zero":          {func(e *Entrada) { e.SessaoID = 0 }, "sessao_id"},
		"sem assentos":         {func(e *Entrada) { e.Assentos = nil }, "assentos"},
		"sete assentos":        {func(e *Entrada) { e.Assentos = []string{"A1", "A2", "A3", "A4", "A5", "A6", "A7"} }, "assentos"},
		"assento repetido":     {func(e *Entrada) { e.Assentos = []string{"A1", "A1"} }, "assentos"},
		"assento mal formado":  {func(e *Entrada) { e.Assentos = []string{"a1"} }, "assentos"},
		"assento com coluna 0": {func(e *Entrada) { e.Assentos = []string{"A0"} }, "assentos"},
	}
	if err := ok.validar(); err != nil {
		t.Fatalf("entrada válida: %v", err)
	}
	for nome, c := range casos {
		in := ok
		in.Assentos = append([]string(nil), ok.Assentos...)
		c.mudar(&in)
		var ev *ErroValidacao
		if err := in.validar(); !errors.As(err, &ev) || strings.Join(ev.Campos, ",") != c.campo {
			t.Errorf("%s: %v", nome, err)
		}
	}
	seis := ok
	seis.Assentos = []string{"A1", "A2", "A3", "A4", "A5", "A6"}
	if err := seis.validar(); err != nil {
		t.Errorf("seis assentos: %v", err)
	}
}
