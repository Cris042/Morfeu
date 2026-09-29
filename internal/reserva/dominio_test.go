package reserva

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2098, 1, 1, 12, 0, 0, 0, time.UTC)

// TestAssentoCodigo cobre CA02: formato F7/F12, sem zero à esquerda.
func TestAssentoCodigo(t *testing.T) {
	for _, ok := range []string{"A1", "F7", "Z50", "B99"} {
		if _, err := NovoAssentoCodigo(ok); err != nil {
			t.Errorf("%s rejeitado: %v", ok, err)
		}
	}
	for _, ruim := range []string{"", "a1", "A0", "A01", "A100", "AA1", "1A", "F7 ", "F-7"} {
		if _, err := NovoAssentoCodigo(ruim); !errors.Is(err, ErrDadosInvalidos) {
			t.Errorf("%q aceito", ruim)
		}
	}
}

// TestNovoLote: 1–6 códigos, sem repetição, ordenados (ordem global — ADR 0008).
func TestNovoLote(t *testing.T) {
	l, err := NovoLote([]string{"F8", "A10", "F7", "A2"})
	if err != nil {
		t.Fatalf("lote válido: %v", err)
	}
	got := ""
	for _, a := range l.Assentos() {
		got += string(a) + ","
	}
	if got != "A10,A2,F7,F8," {
		t.Errorf("ordem = %s", got)
	}
	invalidos := [][]string{nil, {}, {"A1", "A2", "A3", "A4", "A5", "A6", "A7"}, {"A1", "A1"}, {"A1", "x"}}
	for _, c := range invalidos {
		if _, err := NovoLote(c); !errors.Is(err, ErrDadosInvalidos) {
			t.Errorf("lote %v aceito", c)
		}
	}
	if _, err := NovoLote([]string{"A1", "A2", "A3", "A4", "A5", "A6"}); err != nil {
		t.Errorf("6 assentos devem passar: %v", err)
	}
}

func TestLote_ContidoEm(t *testing.T) {
	l, _ := NovoLote([]string{"A1", "B2"})
	if err := l.ContidoEm([]string{"A1", "A2", "B1", "B2"}); err != nil {
		t.Errorf("contido: %v", err)
	}
	if err := l.ContidoEm([]string{"A1", "A2"}); !errors.Is(err, ErrDadosInvalidos) {
		t.Errorf("B2 fora do layout aceito")
	}
}

// TestHold_VivoEEstender cobre CA02: borda exata do prazo e extensão única.
func TestHold_VivoEEstender(t *testing.T) {
	h := reconstituir([16]byte{1}, 7, "F7", t0.Add(TTLHold), 0)
	if !h.Vivo(t0.Add(TTLHold - time.Nanosecond)) {
		t.Error("vivo 1 ns antes do prazo")
	}
	if h.Vivo(t0.Add(TTLHold)) {
		t.Error("no instante exato do prazo o hold já pode ser roubado")
	}
	e, err := h.Estender(t0)
	if err != nil {
		t.Fatalf("1ª extensão: %v", err)
	}
	if !e.ExpiraEm().Equal(t0.Add(TTLHold+ExtensaoHold)) || e.ExtensoesUsadas() != 1 {
		t.Errorf("extensão = %v / %d", e.ExpiraEm(), e.ExtensoesUsadas())
	}
	if h.ExtensoesUsadas() != 0 {
		t.Error("Estender alterou o original")
	}
	if _, err := e.Estender(t0); !errors.Is(err, ErrExtensaoEsgotada) {
		t.Errorf("2ª extensão: %v", err)
	}
	if _, err := h.Estender(t0.Add(TTLHold)); !errors.Is(err, ErrHoldNaoEncontrado) {
		t.Errorf("estender vencido: %v", err)
	}
}

func TestCarrinho(t *testing.T) {
	tok, d, err := NovoCarrinho()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 {
		t.Errorf("token com %d caracteres", len(tok))
	}
	d2, ok := DonoDoToken(tok)
	if !ok || string(d2.hash) != string(d.hash) || len(d.hash) != 32 {
		t.Error("dono do token diverge")
	}
	for _, ruim := range []string{"", "abc", tok + "A", "!!!!"} {
		if _, ok := DonoDoToken(ruim); ok {
			t.Errorf("token %q aceito", ruim)
		}
	}
	if d.chaveLimite() == tok {
		t.Error("chave do limitador não pode ser o token")
	}
}
