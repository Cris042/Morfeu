package sessao

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

var agoraFixo = time.Date(2030, 5, 10, 12, 0, 0, 0, time.UTC)

type filmesFixos struct{}

func (filmesFixos) DuracaoFilmeAtivo(context.Context, int64) (int32, bool, error) {
	return 120, true, nil
}

// TestCalcularFim cobre CA03: fim = início + duração + 20 min de limpeza,
// inclusive na virada do dia.
func TestCalcularFim(t *testing.T) {
	casos := []struct {
		inicio  time.Time
		duracao int32
		fim     time.Time
	}{
		{time.Date(2030, 5, 10, 14, 0, 0, 0, time.UTC), 120, time.Date(2030, 5, 10, 16, 20, 0, 0, time.UTC)},
		{time.Date(2030, 5, 10, 23, 30, 0, 0, time.UTC), 100, time.Date(2030, 5, 11, 1, 30, 0, 0, time.UTC)},
		{time.Date(2030, 5, 10, 10, 0, 0, 0, time.UTC), 1, time.Date(2030, 5, 10, 10, 21, 0, 0, time.UTC)},
	}
	for _, c := range casos {
		if got := CalcularFim(c.inicio, c.duracao); !got.Equal(c.fim) {
			t.Errorf("CalcularFim(%s, %d) = %s, esperado %s", c.inicio, c.duracao, got, c.fim)
		}
	}
}

// TestCriarSessao_ValidacaoAntesDoBanco: início no passado/agora e preço fora
// da faixa são rejeitados antes de qualquer acesso ao banco (q nil).
func TestCriarSessao_ValidacaoAntesDoBanco(t *testing.T) {
	s, err := NovoServico(nil, Config{Filmes: filmesFixos{}, Agora: func() time.Time { return agoraFixo }}, zap.NewNop())
	if err != nil {
		t.Fatalf("NovoServico: %v", err)
	}
	casos := map[string]struct {
		in    EntradaSessao
		campo string
	}{
		"início no passado": {EntradaSessao{Inicio: agoraFixo.Add(-time.Minute), PrecoCentavos: 2000}, "inicio"},
		"início agora":      {EntradaSessao{Inicio: agoraFixo, PrecoCentavos: 2000}, "inicio"},
		"preço 99":          {EntradaSessao{Inicio: agoraFixo.Add(time.Hour), PrecoCentavos: 99}, "preco_centavos"},
		"preço 100001":      {EntradaSessao{Inicio: agoraFixo.Add(time.Hour), PrecoCentavos: 100001}, "preco_centavos"},
	}
	for nome, c := range casos {
		_, err := s.CriarSessao(context.Background(), c.in, "op")
		var ev *ErroValidacao
		if !errors.As(err, &ev) || len(ev.Campos) != 1 || ev.Campos[0] != c.campo {
			t.Errorf("%s: esperava campo %q, veio %v", nome, c.campo, err)
		}
	}
}

func TestNovoServico_ExigePortaDeFilmes(t *testing.T) {
	if _, err := NovoServico(nil, Config{}, zap.NewNop()); err == nil {
		t.Fatal("sem porta de filmes deveria falhar")
	}
}
