package catalogo

import (
	"strings"
	"testing"
)

// TestParaDadosFilme cobre a normalização de dados externos (PRD 0012 RF03):
// HTML removido, limites, pôster só por path no formato esperado (URL montada
// por nós), imdb só se válido, ano da data.
func TestParaDadosFilme(t *testing.T) {
	d := paraDadosFilme(DadosTMDB{
		TmdbID: 1, Titulo: "  <script>x</script>Duna  ", Sinopse: "<p>Paul Atreides</p> " + strings.Repeat("a", 3000),
		DuracaoMin: 155, DataLanc: "2021-09-15", PosterPath: "/d5NXSklXo0qyIYkgV94XAgMIckC.jpg", ImdbID: "tt1160419",
	})
	if d.Titulo != "xDuna" {
		t.Errorf("título sem HTML esperado, veio %q", d.Titulo)
	}
	if d.Sinopse == nil || strings.Contains(*d.Sinopse, "<p>") || len([]rune(*d.Sinopse)) != sinopseMax {
		t.Errorf("sinopse deveria vir sem HTML e truncada em %d", sinopseMax)
	}
	if d.DuracaoMin == nil || *d.DuracaoMin != 155 || d.Ano == nil || *d.Ano != 2021 {
		t.Errorf("duração/ano: %+v", d)
	}
	if d.PosterURL == nil || *d.PosterURL != "https://image.tmdb.org/t/p/w500/d5NXSklXo0qyIYkgV94XAgMIckC.jpg" {
		t.Errorf("pôster: %v", d.PosterURL)
	}
	if d.ImdbID == nil || *d.ImdbID != "tt1160419" {
		t.Errorf("imdb: %v", d.ImdbID)
	}
	if _, err := validar(d); err != nil {
		t.Errorf("dados normalizados deveriam passar na validação: %v", err)
	}
}

func TestParaDadosFilme_ValoresHostis(t *testing.T) {
	for _, path := range []string{"", "../etc/passwd", "//evil.example/x.jpg", "/x.jpg?y=1", "/a b.jpg", "https://evil.example/x.jpg", "/x.svg"} {
		if p := posterDe(path); p != nil {
			t.Errorf("poster_path %q deveria ser descartado, virou %s", path, *p)
		}
	}
	d := paraDadosFilme(DadosTMDB{Titulo: "X", DuracaoMin: 5000, DataLanc: "abc", ImdbID: "javascript:alert(1)"})
	if d.DuracaoMin != nil || d.Ano != nil || d.ImdbID != nil {
		t.Errorf("valores fora de faixa deveriam virar ausentes: %+v", d)
	}
}
