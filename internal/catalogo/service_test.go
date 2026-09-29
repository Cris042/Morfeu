package catalogo

import (
	"errors"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func dadosValidos() DadosFilme {
	return DadosFilme{Titulo: "Filme", DuracaoMin: ptr(int32(120))}
}

// TestValidar_Normalizacao: trim e vazio → ausente.
func TestValidar_Normalizacao(t *testing.T) {
	d := dadosValidos()
	d.Titulo = "  Duna  "
	d.Sinopse = ptr("   ")
	d.ImdbID = ptr(" tt1160419 ")
	out, err := validar(d)
	if err != nil {
		t.Fatalf("válido rejeitado: %v", err)
	}
	if out.Titulo != "Duna" || out.Sinopse != nil || *out.ImdbID != "tt1160419" {
		t.Errorf("normalização errada: %+v", out)
	}
}

// TestValidar_PorCampo cobre CA05 do PRD 0011: um caso por regra, com o
// campo exato em ErroValidacao.Campos.
func TestValidar_PorCampo(t *testing.T) {
	casos := map[string]struct {
		mudar func(*DadosFilme)
		campo string
	}{
		"titulo vazio":       {func(d *DadosFilme) { d.Titulo = "  " }, "titulo"},
		"titulo 256":         {func(d *DadosFilme) { d.Titulo = strings.Repeat("a", 256) }, "titulo"},
		"sinopse 2001":       {func(d *DadosFilme) { d.Sinopse = ptr(strings.Repeat("é", 2001)) }, "sinopse"},
		"sem duracao":        {func(d *DadosFilme) { d.DuracaoMin = nil }, "duracao_min"},
		"duracao 0":          {func(d *DadosFilme) { d.DuracaoMin = ptr(int32(0)) }, "duracao_min"},
		"duracao 1441":       {func(d *DadosFilme) { d.DuracaoMin = ptr(int32(1441)) }, "duracao_min"},
		"ano 1887":           {func(d *DadosFilme) { d.Ano = ptr(int32(1887)) }, "ano"},
		"ano 2101":           {func(d *DadosFilme) { d.Ano = ptr(int32(2101)) }, "ano"},
		"poster outro host":  {func(d *DadosFilme) { d.PosterURL = ptr("https://evil.example/p.jpg") }, "poster_url"},
		"poster http tmdb":   {func(d *DadosFilme) { d.PosterURL = ptr("http://image.tmdb.org/t/p/w500/x.jpg") }, "poster_url"},
		"poster host sufixo": {func(d *DadosFilme) { d.PosterURL = ptr("https://image.tmdb.org.evil.example/x.jpg") }, "poster_url"},
		"imdb formato":       {func(d *DadosFilme) { d.ImdbID = ptr("12345") }, "imdb_id"},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			d := dadosValidos()
			c.mudar(&d)
			_, err := validar(d)
			var ev *ErroValidacao
			if !errors.As(err, &ev) || len(ev.Campos) != 1 || ev.Campos[0] != c.campo {
				t.Fatalf("esperava só o campo %q, veio %v", c.campo, err)
			}
			if !errors.Is(err, ErrDadosInvalidos) {
				t.Error("deveria embrulhar ErrDadosInvalidos")
			}
		})
	}

	ok := dadosValidos()
	ok.PosterURL = ptr("https://image.tmdb.org/t/p/w500/abc.jpg")
	ok.Ano = ptr(int32(2021))
	if _, err := validar(ok); err != nil {
		t.Errorf("pôster do TMDB e ano válidos rejeitados: %v", err)
	}
}
