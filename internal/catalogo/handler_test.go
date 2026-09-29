//go:build integration
// +build integration

package catalogo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/catalogo/db"
)

// Suíte do catálogo pelas rotas HTTP reais (PRD 0011): PG com as migrations
// reais (001 seeds + 002 outbox + 007 filmes) e Redis reais; autorização com o
// middleware de produção (autenticacao.Exigir).
var (
	pool     *pgxpool.Pool
	redisCli *redis.Client
)

var segredoTeste = []byte("segredo-de-teste-catalogo-32-bytes!")

func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "catalogo"},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "postgres:", err)
		os.Exit(1)
	}
	rd, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		_ = pg.Terminate(ctx)
		fmt.Fprintln(os.Stderr, "redis:", err)
		os.Exit(1)
	}

	code := func() int {
		host, _ := pg.Host(ctx)
		porta, _ := pg.MappedPort(ctx, "5432/tcp")
		pool, err = pgxpool.New(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%s/catalogo?sslmode=disable", host, porta.Port()))
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		defer pool.Close()
		for _, arq := range []string{"001_initial_schema.up.sql", "002_outbox_events.up.sql", "007_filmes.up.sql"} {
			ddl, err := os.ReadFile("../../migrations/" + arq)
			if err == nil {
				_, err = pool.Exec(ctx, string(ddl))
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "migration", arq, err)
				return 1
			}
		}
		rh, _ := rd.Host(ctx)
		rp, _ := rd.MappedPort(ctx, "6379/tcp")
		redisCli = redis.NewClient(&redis.Options{Addr: rh + ":" + rp.Port()})
		defer func() { _ = redisCli.Close() }()
		return m.Run()
	}()
	_ = pg.Terminate(ctx)
	_ = rd.Terminate(ctx)
	os.Exit(code)
}

type ambiente struct {
	e        *echo.Echo
	emissor  *autenticacao.Emissor
	operador string
	cliente  string
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	emissor, err := autenticacao.NovoEmissor(autenticacao.ConfigJWT{Segredo: segredoTeste, Kid: "k1", TTL: autenticacao.TTLAccessPadrao})
	if err != nil {
		t.Fatalf("emissor: %v", err)
	}
	s := NovoServico(db.New(pool), pool, cache.NewRedisCache(redisCli, zap.NewNop()), zap.NewNop())
	h := NovoHandler(s, zap.NewNop())
	e := echo.New()
	h.RegistrarRotasPublicas(e)
	h.RegistrarRotasBackoffice(e, autenticacao.Exigir(emissor, autenticacao.PapelOperador))
	op, _, _ := emissor.Emitir(uuid.New(), autenticacao.PapelOperador)
	cl, _, _ := emissor.Emitir(uuid.New(), autenticacao.PapelCliente)
	return &ambiente{e: e, emissor: emissor, operador: op, cliente: cl}
}

func (a *ambiente) req(t *testing.T, metodo, caminho, token string, corpo any) (int, string) {
	t.Helper()
	var body []byte
	if corpo != nil {
		body, _ = json.Marshal(corpo)
	}
	r := httptest.NewRequest(metodo, caminho, bytes.NewReader(body))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		r.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, r)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func (a *ambiente) criar(t *testing.T, titulo string) Filme {
	t.Helper()
	code, corpo := a.req(t, http.MethodPost, "/backoffice/filmes", a.operador,
		map[string]any{"titulo": titulo, "duracao_min": 110, "ano": 2024, "poster_url": "https://image.tmdb.org/t/p/w500/x.jpg"})
	if code != http.StatusCreated {
		t.Fatalf("criar: %d %s", code, corpo)
	}
	var f Filme
	_ = json.Unmarshal([]byte(corpo), &f)
	return f
}

func tituloUnico() string { return "Filme " + uuid.NewString()[:8] }

// TestCartazPublico cobre CA03 e a migração dos seeds (CA01): os 10 filmes da
// migration 001 aparecem em PT; detalhe 200/404.
func TestCartazPublico(t *testing.T) {
	a := novoAmbiente(t)
	code, corpo := a.req(t, http.MethodGet, "/filmes", "", nil)
	if code != http.StatusOK || !strings.Contains(corpo, `"titulo":"The Shawshank Redemption"`) || !strings.Contains(corpo, `"duracao_min":142`) {
		t.Fatalf("cartaz: %d %s", code, corpo)
	}
	if strings.Contains(corpo, `"title"`) {
		t.Error("contrato em PT: não pode haver campo title")
	}
	if code, _ := a.req(t, http.MethodGet, "/filmes/1", "", nil); code != http.StatusOK {
		t.Errorf("detalhe do seed 1: %d", code)
	}
	for _, id := range []string{"999999", "abc", "0"} {
		if code, _ := a.req(t, http.MethodGet, "/filmes/"+id, "", nil); code != http.StatusNotFound {
			t.Errorf("GET /filmes/%s deveria ser 404: %d", id, code)
		}
	}
}

// TestBackoffice_MatrizAutorizacao cobre CA04.
func TestBackoffice_MatrizAutorizacao(t *testing.T) {
	a := novoAmbiente(t)
	f := a.criar(t, tituloUnico())
	emissorVelho, _ := autenticacao.NovoEmissor(autenticacao.ConfigJWT{Segredo: segredoTeste, Kid: "k1", TTL: time.Second,
		Agora: func() time.Time { return time.Now().Add(-time.Hour) }})
	expirado, _, _ := emissorVelho.Emitir(uuid.New(), autenticacao.PapelOperador)

	rotas := []struct {
		metodo, caminho string
		corpo           any
		sucesso         int
	}{
		{http.MethodGet, "/backoffice/filmes", nil, http.StatusOK},
		{http.MethodPost, "/backoffice/filmes", map[string]any{"titulo": tituloUnico(), "duracao_min": 90}, http.StatusCreated},
		{http.MethodPut, fmt.Sprintf("/backoffice/filmes/%d", f.ID), map[string]any{"titulo": "Editado", "duracao_min": 95}, http.StatusOK},
		{http.MethodPost, fmt.Sprintf("/backoffice/filmes/%d/arquivar", f.ID), nil, http.StatusNoContent},
	}
	tokens := map[string]struct {
		token string
		want  func(int) int
	}{
		"sem token": {"", func(int) int { return http.StatusUnauthorized }},
		"expirado":  {expirado, func(int) int { return http.StatusUnauthorized }},
		"cliente":   {a.cliente, func(int) int { return http.StatusForbidden }},
		"operador":  {a.operador, func(s int) int { return s }},
	}
	for nome, tk := range tokens {
		for _, r := range rotas {
			code, corpo := a.req(t, r.metodo, r.caminho, tk.token, r.corpo)
			if want := tk.want(r.sucesso); code != want {
				t.Errorf("%s %s %s: %d (esperado %d) %s", nome, r.metodo, r.caminho, code, want, corpo)
			}
		}
	}
}

// TestBackoffice_CRUDEArquivamento cobre RF03/RN01: edição, arquivamento
// idempotente, arquivado fora do cartaz e no backoffice, 404s e validação.
func TestBackoffice_CRUDEArquivamento(t *testing.T) {
	a := novoAmbiente(t)
	f := a.criar(t, tituloUnico())

	code, corpo := a.req(t, http.MethodPut, fmt.Sprintf("/backoffice/filmes/%d", f.ID), a.operador, map[string]any{"titulo": "  Título Novo  ", "duracao_min": 131})
	if code != http.StatusOK || !strings.Contains(corpo, `"titulo":"Título Novo"`) || !strings.Contains(corpo, `"duracao_min":131`) {
		t.Fatalf("editar: %d %s", code, corpo)
	}
	if code, _ := a.req(t, http.MethodPut, "/backoffice/filmes/999999", a.operador, map[string]any{"titulo": "X", "duracao_min": 90}); code != http.StatusNotFound {
		t.Errorf("editar inexistente: %d", code)
	}
	if code, corpo := a.req(t, http.MethodPost, "/backoffice/filmes", a.operador, map[string]any{"titulo": "Sem duração"}); code != http.StatusBadRequest || !strings.Contains(corpo, "duracao_min") {
		t.Errorf("criar sem duração deveria ser 400 com o campo: %d %s", code, corpo)
	}

	caminho := fmt.Sprintf("/backoffice/filmes/%d/arquivar", f.ID)
	for i := 0; i < 2; i++ {
		if code, _ := a.req(t, http.MethodPost, caminho, a.operador, nil); code != http.StatusNoContent {
			t.Fatalf("arquivar (%dª vez) deveria ser 204: %d", i+1, code)
		}
	}
	if code, _ := a.req(t, http.MethodPost, "/backoffice/filmes/999999/arquivar", a.operador, nil); code != http.StatusNotFound {
		t.Errorf("arquivar inexistente: %d", code)
	}
	if code, _ := a.req(t, http.MethodGet, fmt.Sprintf("/filmes/%d", f.ID), "", nil); code != http.StatusNotFound {
		t.Errorf("arquivado não pode aparecer no detalhe público: %d", code)
	}
	if _, corpo := a.req(t, http.MethodGet, "/filmes", "", nil); strings.Contains(corpo, fmt.Sprintf(`"id":%d,`, f.ID)) {
		t.Error("arquivado não pode aparecer no cartaz")
	}
	_, corpo = a.req(t, http.MethodGet, "/backoffice/filmes", a.operador, nil)
	if !strings.Contains(corpo, fmt.Sprintf(`"id":%d,`, f.ID)) || !strings.Contains(corpo, "arquivado_em") {
		t.Error("backoffice deve listar o arquivado com arquivado_em")
	}
}

// TestCriar_ConcorrenteSemColisao cobre CA02 (IDENTITY, fim do MAX+1) e o
// evento na outbox na mesma TX.
func TestCriar_ConcorrenteSemColisao(t *testing.T) {
	a := novoAmbiente(t)
	const n = 8
	ids := make([]int64, n)
	var wg sync.WaitGroup
	largada := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-largada
			ids[i] = a.criar(t, tituloUnico()).ID
		}(i)
	}
	close(largada)
	wg.Wait()
	vistos := map[int64]bool{}
	for _, id := range ids {
		if id <= 10 || vistos[id] {
			t.Fatalf("ids colidiram ou reusaram os seeds: %v", ids)
		}
		vistos[id] = true
	}
	var eventos int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM outbox_events WHERE event_type = 'catalogo.filme_criado' AND aggregate_id = ANY($1)",
		func() []string {
			out := make([]string, 0, n)
			for _, id := range ids {
				out = append(out, fmt.Sprint(id))
			}
			return out
		}()).Scan(&eventos); err != nil || eventos != n {
		t.Errorf("esperava %d eventos na outbox, veio %d (err=%v)", n, eventos, err)
	}
}

// TestCache_InvalidacaoSincrona cobre CA06: cada escrita apaga a chave do
// cartaz no Redis real; a leitura seguinte repopula sem o arquivado.
func TestCache_InvalidacaoSincrona(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	existe := func() bool { n, _ := redisCli.Exists(ctx, chaveCachePublico).Result(); return n == 1 }
	popular := func() {
		t.Helper()
		a.req(t, http.MethodGet, "/filmes", "", nil)
		if !existe() {
			t.Fatal("GET /filmes deveria popular o cache")
		}
	}

	popular()
	f := a.criar(t, tituloUnico())
	if existe() {
		t.Error("criar deveria apagar o cartaz em cache")
	}
	popular()
	a.req(t, http.MethodPut, fmt.Sprintf("/backoffice/filmes/%d", f.ID), a.operador, map[string]any{"titulo": "Editado cache", "duracao_min": 100})
	if existe() {
		t.Error("editar deveria apagar o cartaz em cache")
	}
	popular()
	a.req(t, http.MethodPost, fmt.Sprintf("/backoffice/filmes/%d/arquivar", f.ID), a.operador, nil)
	if existe() {
		t.Error("arquivar deveria apagar o cartaz em cache")
	}
	_, corpo := a.req(t, http.MethodGet, "/filmes", "", nil)
	if !existe() || strings.Contains(corpo, fmt.Sprintf(`"id":%d,`, f.ID)) {
		t.Error("leitura após arquivar deve repopular o cache sem o arquivado")
	}
}
