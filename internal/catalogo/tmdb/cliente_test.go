package tmdb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/catalogo"
)

const tokenTeste = "token-de-teste-que-nao-pode-vazar" //nolint:gosec // G101: valor fictício de teste

// fakeTMDB responde conforme a sequência de status (o último se repete) e
// conta as tentativas; valida o header Authorization.
func fakeTMDB(t *testing.T, statuses []int, corpo string, retryAfter string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		if r.Header.Get("Authorization") != "Bearer "+tokenTeste {
			t.Errorf("Authorization inesperado: %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("language") != "pt-BR" {
			t.Errorf("language deveria ser pt-BR: %s", r.URL.RawQuery)
		}
		st := statuses[len(statuses)-1]
		if i < len(statuses) {
			st = statuses[i]
		}
		if st == http.StatusTooManyRequests && retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(st)
		if st == http.StatusOK {
			_, _ = fmt.Fprint(w, corpo)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

type dormidor struct{ esperas []time.Duration }

func (d *dormidor) dormir(ctx context.Context, e time.Duration) error {
	d.esperas = append(d.esperas, e)
	return ctx.Err()
}

func novo(t *testing.T, base string) (*Cliente, *dormidor) {
	t.Helper()
	d := &dormidor{}
	c, err := NovoCliente(Config{Token: tokenTeste, BaseURL: base, Dormir: d.dormir}, zap.NewNop())
	if err != nil {
		t.Fatalf("NovoCliente: %v", err)
	}
	return c, d
}

const detalheOK = `{"id":603,"title":"Matrix","overview":"Um hacker descobre a verdade.","runtime":136,"release_date":"1999-03-30","poster_path":"/abc123.jpg","imdb_id":"tt0133093"}`

func semToken(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), tokenTeste) {
		t.Errorf("token vazou no erro: %v", err)
	}
}

// TestDetalhar_Sucesso cobre o mapeamento e o header de autenticação.
func TestDetalhar_Sucesso(t *testing.T) {
	srv, n := fakeTMDB(t, []int{200}, detalheOK, "")
	c, _ := novo(t, srv.URL)
	d, err := c.Detalhar(context.Background(), 603)
	if err != nil {
		t.Fatalf("Detalhar: %v", err)
	}
	if n.Load() != 1 || d.Titulo != "Matrix" || d.DuracaoMin != 136 || d.PosterPath != "/abc123.jpg" || d.ImdbID != "tt0133093" || d.DataLanc != "1999-03-30" {
		t.Errorf("mapeamento/tentativas inesperados (%d): %+v", n.Load(), d)
	}
}

// TestRetry cobre CA04: tentativas por classe de erro.
func TestRetry(t *testing.T) {
	casos := []struct {
		nome        string
		statuses    []int
		retryAfter  string
		tentativas  int32
		erro        error
		primeiraEsp time.Duration
	}{
		{"429 com Retry-After e depois ok", []int{429, 200}, "1", 2, nil, time.Second},
		{"5xx contínuo esgota 3 tentativas", []int{503}, "", 3, catalogo.ErrTMDBIndisponivel, 0},
		{"500 depois ok", []int{500, 200}, "", 2, nil, 0},
		{"400 sem retry", []int{400}, "", 1, catalogo.ErrTMDBIndisponivel, 0},
		{"404 sem retry", []int{404}, "", 1, catalogo.ErrTMDBNaoEncontrado, 0},
		{"Retry-After acima do teto", []int{429, 200}, "60", 2, nil, tetoRetryAfter},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			srv, n := fakeTMDB(t, c.statuses, detalheOK, c.retryAfter)
			cli, d := novo(t, srv.URL)
			_, err := cli.Detalhar(context.Background(), 603)
			semToken(t, err)
			conferirTentativas(t, n.Load(), c.tentativas, err, c.erro, d.esperas, c.primeiraEsp)
		})
	}
}

// conferirTentativas valida nº de tentativas, erro esperado e esperas.
func conferirTentativas(t *testing.T, tentativas, esperado int32, err, erroEsperado error, esperas []time.Duration, primeiraEsp time.Duration) {
	t.Helper()
	if tentativas != esperado {
		t.Errorf("tentativas = %d, esperado %d", tentativas, esperado)
	}
	if (erroEsperado == nil) != (err == nil) || (erroEsperado != nil && !errors.Is(err, erroEsperado)) {
		t.Errorf("erro = %v, esperado %v", err, erroEsperado)
	}
	if primeiraEsp > 0 && (len(esperas) == 0 || esperas[0] != primeiraEsp) {
		t.Errorf("1ª espera deveria ser %s (Retry-After), veio %v", primeiraEsp, esperas)
	}
	if esperado == 3 && len(esperas) != 2 {
		t.Errorf("esperava 2 esperas entre 3 tentativas, veio %v", esperas)
	}
}

// TestJSONMalformadoETimeout: sem pânico, erro de indisponibilidade.
func TestJSONMalformadoETimeout(t *testing.T) {
	srv, n := fakeTMDB(t, []int{200}, `{"id": 603, "title": `, "")
	c, _ := novo(t, srv.URL)
	_, err := c.Detalhar(context.Background(), 603)
	if !errors.Is(err, catalogo.ErrTMDBIndisponivel) || n.Load() != 1 {
		t.Errorf("JSON malformado: err=%v tentativas=%d", err, n.Load())
	}

	lento := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(lento.Close)
	c2, _ := novo(t, lento.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	inicio := time.Now()
	_, err = c2.Detalhar(ctx, 603)
	semToken(t, err)
	if !errors.Is(err, catalogo.ErrTMDBIndisponivel) || time.Since(inicio) > 3*time.Second {
		t.Errorf("timeout deveria falhar rápido como indisponível: %v (%s)", err, time.Since(inicio))
	}
}

// TestBuscar: termo escapado, limite de 20 e pôster/ano derivados.
func TestBuscar(t *testing.T) {
	var itens []string
	for i := 1; i <= 25; i++ {
		itens = append(itens, fmt.Sprintf(`{"id":%d,"title":"<b>Filme %d</b>","release_date":"2020-01-01","poster_path":"/p%d.jpg"}`, i, i, i))
	}
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("query")
		_, _ = fmt.Fprintf(w, `{"results":[%s]}`, strings.Join(itens, ","))
	}))
	t.Cleanup(srv.Close)
	c, _ := novo(t, srv.URL)
	res, err := c.Buscar(context.Background(), "o poderoso & chefão")
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if query != "o poderoso & chefão" || len(res) != 20 {
		t.Fatalf("query=%q len=%d", query, len(res))
	}
	if res[0].Titulo != "Filme 1" || res[0].Ano == nil || *res[0].Ano != 2020 || res[0].PosterURL == nil || *res[0].PosterURL != "https://image.tmdb.org/t/p/w500/p1.jpg" {
		t.Errorf("resultado mal normalizado: %+v", res[0])
	}
}

func TestNovoCliente_TokenObrigatorio(t *testing.T) {
	if _, err := NovoCliente(Config{}, zap.NewNop()); err == nil {
		t.Fatal("token vazio deveria falhar")
	}
}

// TestNenhumTesteChamaAPIReal é o guarda-chuva do RNF01 do PRD 0012: nenhum
// arquivo de teste do repositório pode apontar para o host real do TMDB.
func TestNenhumTesteChamaAPIReal(t *testing.T) {
	hostReal := "api.themoviedb" + ".org" // montado para este arquivo não casar consigo mesmo
	raiz := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(raiz, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // G304: varredura dos próprios arquivos de teste do repo
		if err != nil {
			return err
		}
		if strings.Contains(string(b), hostReal) {
			t.Errorf("%s referencia o host real do TMDB — use httptest", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrer testes: %v", err)
	}
}
