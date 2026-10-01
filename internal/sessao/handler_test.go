//go:build integration
// +build integration

package sessao

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/catalogo"
	catalogodb "github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/sessao/db"
)

// Suíte do módulo sessao pelas rotas reais (PRD 0013): PG com as migrations
// reais (001 seeds, 002 outbox, 007 filmes, 008 salas/sessoes — EXCLUDE de
// verdade) e o catálogo REAL como porta de duração (ADR 0003: main injeta).
var (
	pool     *pgxpool.Pool
	redisCli *redis.Client
)

var segredoTeste = []byte("segredo-de-teste-sessao-32-bytes!!!")

func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "sessao"},
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
		rh, _ := rd.Host(ctx)
		rp, _ := rd.MappedPort(ctx, "6379/tcp")
		redisCli = redis.NewClient(&redis.Options{Addr: rh + ":" + rp.Port()})
		defer func() { _ = redisCli.Close() }()
		host, _ := pg.Host(ctx)
		porta, _ := pg.MappedPort(ctx, "5432/tcp")
		pool, err = pgxpool.New(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%s/sessao?sslmode=disable", host, porta.Port()))
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		defer pool.Close()
		for _, arq := range []string{"001_initial_schema.up.sql", "002_outbox_events.up.sql", "007_filmes.up.sql", "008_salas_sessoes.up.sql", "017_eventos_auditoria.up.sql"} {
			ddl, err := os.ReadFile("../../migrations/" + arq)
			if err == nil {
				_, err = pool.Exec(ctx, string(ddl))
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "migration", arq, err)
				return 1
			}
		}
		return m.Run()
	}()
	_ = pg.Terminate(ctx)
	_ = rd.Terminate(ctx)
	os.Exit(code)
}

// semPedidos é a porta do pedido sem pedidos (PRD 0036).
type semPedidos struct{}

func (semPedidos) CancelarPedidosDaSessao(context.Context, outbox.Tx, int64) (int64, error) {
	return 0, nil
}

type ambiente struct {
	e         *echo.Echo
	operador  string
	cliente   string
	conflitos *atomic.Int32
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	return montarAmbiente(t, time.Now)
}

// montarAmbiente permite fixar o relógio do serviço (sessões "que começam"
// durante o TTL do cache, mapa de sessão já iniciada).
func montarAmbiente(t *testing.T, agora func() time.Time) *ambiente {
	t.Helper()
	emissor, err := autenticacao.NovoEmissor(autenticacao.ConfigJWT{Segredo: segredoTeste, Kid: "k1", TTL: autenticacao.TTLAccessPadrao})
	if err != nil {
		t.Fatalf("emissor: %v", err)
	}
	var conflitos atomic.Int32
	filmes := catalogo.NovoServico(catalogodb.New(pool), pool, nil, zap.NewNop())
	// zaptest: logs do serviço/handler aparecem na saída do teste que falhar
	// (diagnóstico do SQLSTATE em erro inesperado — CI do PR #38).
	logTeste := zaptest.NewLogger(t, zaptest.Level(zapcore.WarnLevel))
	s, err := NovoServico(db.New(pool), Config{Pool: pool, Filmes: filmes, AoConflito: func(context.Context) { conflitos.Add(1) },
		Agora: agora, Cache: cache.NewRedisCache(redisCli, zap.NewNop())}, logTeste)
	if err != nil {
		t.Fatalf("serviço: %v", err)
	}
	s.LigarPedidos(semPedidos{}, nil) // a porta real é testada na suíte do pedido
	e := echo.New()
	NovoHandler(s, logTeste).RegistrarRotasPublicas(e)
	NovoHandler(s, logTeste).RegistrarRotasBackoffice(e, autenticacao.Exigir(emissor, autenticacao.PapelOperador),
		func(c echo.Context) string { id, _ := autenticacao.UsuarioID(c); return id.String() })
	op, _, _ := emissor.Emitir(uuid.New(), autenticacao.PapelOperador)
	cl, _, _ := emissor.Emitir(uuid.New(), autenticacao.PapelCliente)
	return &ambiente{e: e, operador: op, cliente: cl, conflitos: &conflitos}
}

func (a *ambiente) req(t *testing.T, metodo, caminho, token string, corpo any) (int, string) {
	t.Helper()
	var body []byte
	switch c := corpo.(type) {
	case nil:
	case string:
		body = []byte(c)
	default:
		body, _ = json.Marshal(c)
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

const layoutPadrao = `{"fileiras":5,"colunas":8,"vaos":[{"fileira":"C","coluna":4}],"pcd":[{"fileira":"A","coluna":1}]}`

func (a *ambiente) novaSala(t *testing.T) int64 {
	t.Helper()
	code, corpo := a.req(t, http.MethodPost, "/backoffice/salas", a.operador,
		`{"nome":"Sala `+uuid.NewString()[:8]+`","layout":`+layoutPadrao+`}`)
	if code != http.StatusCreated {
		t.Fatalf("criar sala: %d %s", code, corpo)
	}
	var s Sala
	_ = json.Unmarshal([]byte(corpo), &s)
	return s.ID
}

// base: horário futuro fixo e distante (independe do relógio real).
var base = time.Date(2099, 3, 1, 14, 0, 0, 0, time.UTC)

func (a *ambiente) criarSessao(t *testing.T, filme, sala int64, inicio time.Time) (int, string) {
	t.Helper()
	return a.req(t, http.MethodPost, "/backoffice/sessoes", a.operador,
		map[string]any{"filme_id": filme, "sala_id": sala, "inicio": inicio.Format(time.RFC3339), "preco_centavos": 3200})
}

// TestSessao_FimSnapshotEBordas cobre CA03/CA04. O filme 1 (seed) tem 142
// min: fim = início + 142 + 20 = início + 2h42.
func TestSessao_FimSnapshotEBordas(t *testing.T) {
	a := novoAmbiente(t)
	sala, outraSala := a.novaSala(t), a.novaSala(t)

	code, corpo := a.criarSessao(t, 1, sala, base)
	if code != http.StatusCreated {
		t.Fatalf("1ª sessão: %d %s", code, corpo)
	}
	var s Sessao
	_ = json.Unmarshal([]byte(corpo), &s)
	fimEsperado := base.Add(142*time.Minute + IntervaloLimpeza)
	if !s.Fim.Equal(fimEsperado) || s.DuracaoMin != 142 || s.Status != "agendada" {
		t.Fatalf("fim/snapshot errados: %+v (esperado fim %s)", s, fimEsperado)
	}

	casos := []struct {
		nome   string
		sala   int64
		inicio time.Time
		status int
	}{
		{"parcial no início", sala, base.Add(-time.Hour), http.StatusConflict},
		{"parcial no fim (dentro da limpeza)", sala, fimEsperado.Add(-time.Minute), http.StatusConflict},
		{"contida", sala, base.Add(30 * time.Minute), http.StatusConflict},
		{"encostada no fim", sala, fimEsperado, http.StatusCreated},
		{"outra sala no mesmo horário", outraSala, base, http.StatusCreated},
	}
	for _, c := range casos {
		code, corpo := a.criarSessao(t, 1, c.sala, c.inicio)
		if code != c.status {
			t.Errorf("%s: %d %s (esperado %d)", c.nome, code, corpo, c.status)
		}
		if c.status == http.StatusConflict && !strings.Contains(corpo, `"conflitante"`) {
			t.Errorf("%s: 409 deve trazer o horário conflitante: %s", c.nome, corpo)
		}
	}
	if a.conflitos.Load() != 3 {
		t.Errorf("callback de conflito chamado %d vezes, esperado 3", a.conflitos.Load())
	}

	// Cancelar libera o horário: o mesmo intervalo da cancelada (que encosta
	// na sessão das 16:42) volta a ser aceito.
	// Sem pedidos (a porta do pedido é coberta na suíte do pedido — PRD 0036).
	code, corpo = a.req(t, http.MethodPost, fmt.Sprintf("/backoffice/sessoes/%d/cancelar", s.ID), a.operador, nil)
	if code != http.StatusOK || corpo != `{"pedidos_estornados":0}` {
		t.Fatalf("cancelar: %d %s", code, corpo)
	}
	if code, corpo = a.req(t, http.MethodPost, fmt.Sprintf("/backoffice/sessoes/%d/cancelar", s.ID), a.operador, nil); code != http.StatusOK || corpo != `{"pedidos_estornados":0}` {
		t.Errorf("cancelar de novo deveria ser idempotente (200, 0): %d %s", code, corpo)
	}
	// Trilha (PRD 0037): criação e cancelamento, uma vez cada.
	var trilha string
	_ = pool.QueryRow(context.Background(), `SELECT string_agg(acao, ',' ORDER BY id) FROM eventos_auditoria WHERE alvo_tipo = 'sessao' AND alvo_id = $1`,
		fmt.Sprint(s.ID)).Scan(&trilha)
	if trilha != "sessao_criada,sessao_cancelada" {
		t.Errorf("trilha da sessão: %q", trilha)
	}
	if code, corpo := a.criarSessao(t, 1, sala, base); code != http.StatusCreated {
		t.Errorf("horário da cancelada deveria estar livre: %d %s", code, corpo)
	}
}

// TestSessao_ConcorrenciaConflitante cobre CA05: exatamente 1 sucesso.
func TestSessao_ConcorrenciaConflitante(t *testing.T) {
	a := novoAmbiente(t)
	sala := a.novaSala(t)
	// 20 rodadas: o CI ARM64 revelou 500 por deadlock (40P01) na EXCLUDE sob
	// concorrência — agora repetido pelo serviço; mais rodadas exercitam isso.
	for rodada := 0; rodada < 20; rodada++ {
		inicio := base.AddDate(0, 0, rodada+1)
		largada := make(chan struct{})
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i := range codes {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-largada
				codes[i], _ = a.criarSessao(t, 2, sala, inicio.Add(time.Duration(i)*10*time.Minute))
			}(i)
		}
		close(largada)
		wg.Wait()
		if !((codes[0] == 201 && codes[1] == 409) || (codes[0] == 409 && codes[1] == 201)) {
			t.Fatalf("rodada %d: esperava um 201 e um 409, veio %v", rodada, codes)
		}
	}
}

// TestSessao_ValidacoesEPorta cobre CA03/CA06 com a porta REAL do catálogo.
func TestSessao_ValidacoesEPorta(t *testing.T) {
	a := novoAmbiente(t)
	sala := a.novaSala(t)
	if _, err := pool.Exec(context.Background(), "UPDATE filmes SET arquivado_em = now() WHERE id = 3"); err != nil {
		t.Fatalf("arquivar filme: %v", err)
	}
	casos := []struct {
		nome   string
		corpo  map[string]any
		status int
		erro   string
	}{
		{"filme arquivado", map[string]any{"filme_id": 3, "sala_id": sala, "inicio": base.Format(time.RFC3339), "preco_centavos": 3000}, 422, "filme_indisponivel"},
		{"filme inexistente", map[string]any{"filme_id": 999999, "sala_id": sala, "inicio": base.Format(time.RFC3339), "preco_centavos": 3000}, 422, "filme_indisponivel"},
		{"sala inexistente", map[string]any{"filme_id": 1, "sala_id": 999999, "inicio": base.Format(time.RFC3339), "preco_centavos": 3000}, 422, "sala_inexistente"},
		{"passado", map[string]any{"filme_id": 1, "sala_id": sala, "inicio": "2001-01-01T10:00:00Z", "preco_centavos": 3000}, 400, "inicio"},
		{"preço 0", map[string]any{"filme_id": 1, "sala_id": sala, "inicio": base.Format(time.RFC3339), "preco_centavos": 0}, 400, "preco_centavos"},
		{"início não RFC3339", map[string]any{"filme_id": 1, "sala_id": sala, "inicio": "amanhã", "preco_centavos": 3000}, 400, "inicio"},
		{"campo desconhecido", map[string]any{"filme_id": 1, "sala_id": sala, "inicio": base.Format(time.RFC3339), "preco_centavos": 3000, "total": 1}, 400, "requisicao_invalida"},
	}
	for _, c := range casos {
		code, corpo := a.req(t, http.MethodPost, "/backoffice/sessoes", a.operador, c.corpo)
		if code != c.status || !strings.Contains(corpo, c.erro) {
			t.Errorf("%s: %d %s (esperado %d %s)", c.nome, code, corpo, c.status, c.erro)
		}
	}
}

// TestSala_ImutabilidadeENome cobre CA07 e o nome único.
func TestSala_ImutabilidadeENome(t *testing.T) {
	a := novoAmbiente(t)
	livre := a.novaSala(t)
	nova := `{"nome":"Renomeada ` + uuid.NewString()[:6] + `","layout":{"fileiras":4,"colunas":8}}`
	if code, corpo := a.req(t, http.MethodPut, fmt.Sprintf("/backoffice/salas/%d", livre), a.operador, nova); code != http.StatusOK {
		t.Fatalf("sala sem sessão deveria aceitar layout novo: %d %s", code, corpo)
	}

	emUso := a.novaSala(t)
	if code, corpo := a.criarSessao(t, 1, emUso, base.AddDate(0, 1, 0)); code != http.StatusCreated {
		t.Fatalf("sessão: %d %s", code, corpo)
	}
	outroLayout := `{"nome":"X ` + uuid.NewString()[:6] + `","layout":{"fileiras":2,"colunas":2}}`
	if code, corpo := a.req(t, http.MethodPut, fmt.Sprintf("/backoffice/salas/%d", emUso), a.operador, outroLayout); code != http.StatusConflict || !strings.Contains(corpo, "layout_em_uso") {
		t.Errorf("layout com sessão futura deveria ser 409 layout_em_uso: %d %s", code, corpo)
	}
	soNome := `{"nome":"Só nome ` + uuid.NewString()[:6] + `","layout":` + layoutPadrao + `}`
	if code, corpo := a.req(t, http.MethodPut, fmt.Sprintf("/backoffice/salas/%d", emUso), a.operador, soNome); code != http.StatusOK {
		t.Errorf("trocar só o nome deveria ser permitido: %d %s", code, corpo)
	}

	dup := `{"nome":"Duplicada","layout":` + layoutPadrao + `}`
	a.req(t, http.MethodPost, "/backoffice/salas", a.operador, dup)
	if code, _ := a.req(t, http.MethodPost, "/backoffice/salas", a.operador, dup); code != http.StatusConflict {
		t.Errorf("nome duplicado deveria ser 409: %d", code)
	}
	if code, _ := a.req(t, http.MethodPost, "/backoffice/salas", a.operador, `{"nome":"Ruim","layout":{"fileiras":99,"colunas":2}}`); code != http.StatusBadRequest {
		t.Errorf("layout inválido deveria ser 400: %d", code)
	}
}

// TestSessao_MatrizAutorizacao cobre CA08.
func TestSessao_MatrizAutorizacao(t *testing.T) {
	a := novoAmbiente(t)
	sala := a.novaSala(t)
	rotas := []struct{ metodo, caminho string }{
		{http.MethodGet, "/backoffice/salas"},
		{http.MethodPost, "/backoffice/salas"},
		{http.MethodPut, fmt.Sprintf("/backoffice/salas/%d", sala)},
		{http.MethodGet, "/backoffice/sessoes"},
		{http.MethodPost, "/backoffice/sessoes"},
		{http.MethodPost, "/backoffice/sessoes/1/cancelar"},
	}
	for _, r := range rotas {
		if code, _ := a.req(t, r.metodo, r.caminho, "", nil); code != http.StatusUnauthorized {
			t.Errorf("sem token %s %s: %d", r.metodo, r.caminho, code)
		}
		if code, _ := a.req(t, r.metodo, r.caminho, a.cliente, nil); code != http.StatusForbidden {
			t.Errorf("cliente %s %s: %d", r.metodo, r.caminho, code)
		}
	}
	if code, _ := a.req(t, http.MethodGet, "/backoffice/sessoes", a.operador, nil); code != http.StatusOK {
		t.Errorf("operador GET sessões: %d", code)
	}
	if code, _ := a.req(t, http.MethodPost, "/backoffice/sessoes/999999/cancelar", a.operador, nil); code != http.StatusNotFound {
		t.Errorf("cancelar inexistente: %d", code)
	}
}

func sessaoCriada(t *testing.T, a *ambiente, filme, sala int64, inicio time.Time) Sessao {
	t.Helper()
	code, corpo := a.criarSessao(t, filme, sala, inicio)
	if code != http.StatusCreated {
		t.Fatalf("criar sessão: %d %s", code, corpo)
	}
	var s Sessao
	_ = json.Unmarshal([]byte(corpo), &s)
	return s
}

// TestPublico_SessoesDoFilmeECache cobre CA01/CA02 do PRD 0014.
func TestPublico_SessoesDoFilmeECache(t *testing.T) {
	a := novoAmbiente(t)
	sala := a.novaSala(t)
	const filme = 5
	dia := time.Date(2099, 7, 1, 0, 0, 0, 0, time.UTC)
	s1 := sessaoCriada(t, a, filme, sala, dia.Add(10*time.Hour))
	s2 := sessaoCriada(t, a, filme, sala, dia.Add(18*time.Hour))
	outroFilme := sessaoCriada(t, a, 6, sala, dia.Add(14*time.Hour))
	chave := chaveCacheFilme(filme)
	ctx := context.Background()

	code, corpo := a.req(t, http.MethodGet, fmt.Sprintf("/filmes/%d/sessoes", filme), "", nil)
	if code != http.StatusOK {
		t.Fatalf("público: %d %s", code, corpo)
	}
	var lista []SessaoPublica
	_ = json.Unmarshal([]byte(corpo), &lista)
	if len(lista) != 2 || lista[0].ID != s1.ID || lista[1].ID != s2.ID || lista[0].SalaNome == "" {
		t.Fatalf("esperava [s1, s2] em ordem, veio %+v", lista)
	}
	if strings.Contains(corpo, "titulo") || strings.Contains(corpo, "filme_id") || strings.Contains(corpo, fmt.Sprint(outroFilme.ID)+",") {
		t.Errorf("contrato público vazou campo de filme ou sessão de outro filme: %s", corpo)
	}
	if n, _ := redisCli.Exists(ctx, chave).Result(); n != 1 {
		t.Fatal("leitura deveria popular o cache")
	}

	// Cancelar invalida; a cancelada some.
	a.req(t, http.MethodPost, fmt.Sprintf("/backoffice/sessoes/%d/cancelar", s1.ID), a.operador, nil)
	if n, _ := redisCli.Exists(ctx, chave).Result(); n != 0 {
		t.Error("cancelar deveria invalidar o cache do filme")
	}
	_, corpo = a.req(t, http.MethodGet, fmt.Sprintf("/filmes/%d/sessoes", filme), "", nil)
	if strings.Contains(corpo, fmt.Sprintf(`"id":%d,`, s1.ID)) {
		t.Error("sessão cancelada não pode aparecer")
	}
	// Criar invalida.
	sessaoCriada(t, a, filme, sala, dia.Add(22*time.Hour))
	if n, _ := redisCli.Exists(ctx, chave).Result(); n != 0 {
		t.Error("criar deveria invalidar o cache do filme")
	}

	// Sessão que "começa" durante o TTL: o relógio avança para depois de s2
	// e a resposta, servida do cache, já não a mostra.
	a.req(t, http.MethodGet, fmt.Sprintf("/filmes/%d/sessoes", filme), "", nil) // repopula
	depois := montarAmbiente(t, func() time.Time { return s2.Inicio.Add(time.Minute) })
	_, corpo = depois.req(t, http.MethodGet, fmt.Sprintf("/filmes/%d/sessoes", filme), "", nil)
	if strings.Contains(corpo, fmt.Sprintf(`"id":%d,`, s2.ID)) {
		t.Errorf("sessão já iniciada não pode vir do cache: %s", corpo)
	}

	if code, corpo := a.req(t, http.MethodGet, "/filmes/999999/sessoes", "", nil); code != http.StatusOK || corpo != "[]" {
		t.Errorf("filme sem sessões deveria devolver []: %d %s", code, corpo)
	}
}

// TestPublico_Mapa cobre CA03.
func TestPublico_Mapa(t *testing.T) {
	a := novoAmbiente(t)
	sala := a.novaSala(t) // layoutPadrao: 5×8, vão C4, PCD A1
	s := sessaoCriada(t, a, 7, sala, time.Date(2099, 8, 1, 15, 0, 0, 0, time.UTC))

	code, corpo := a.req(t, http.MethodGet, fmt.Sprintf("/sessoes/%d/mapa", s.ID), "", nil)
	if code != http.StatusOK {
		t.Fatalf("mapa: %d %s", code, corpo)
	}
	var m MapaSessao
	_ = json.Unmarshal([]byte(corpo), &m)
	if m.Fileiras != 5 || m.Colunas != 8 || len(m.Assentos) != 39 || m.Assentos[0].Codigo != "A1" || !m.Assentos[0].PCD {
		t.Fatalf("mapa inesperado: fileiras=%d colunas=%d assentos=%d primeiro=%+v", m.Fileiras, m.Colunas, len(m.Assentos), m.Assentos[0])
	}
	if strings.Contains(corpo, `"C4"`) {
		t.Error("vão não pode virar assento")
	}

	depois := montarAmbiente(t, func() time.Time { return s.Inicio.Add(time.Minute) })
	if code, _ := depois.req(t, http.MethodGet, fmt.Sprintf("/sessoes/%d/mapa", s.ID), "", nil); code != http.StatusNotFound {
		t.Errorf("mapa de sessão já iniciada deveria ser 404: %d", code)
	}
	// Sessão que já começou não é cancelada (PRD 0036 RF10).
	if code, corpo := depois.req(t, http.MethodPost, fmt.Sprintf("/backoffice/sessoes/%d/cancelar", s.ID), depois.operador, nil); code != http.StatusConflict || !strings.Contains(corpo, "sessao_iniciada") {
		t.Errorf("cancelar sessão iniciada deveria ser 409: %d %s", code, corpo)
	}
	a.req(t, http.MethodPost, fmt.Sprintf("/backoffice/sessoes/%d/cancelar", s.ID), a.operador, nil)
	if code, _ := a.req(t, http.MethodGet, fmt.Sprintf("/sessoes/%d/mapa", s.ID), "", nil); code != http.StatusNotFound {
		t.Errorf("mapa de cancelada deveria ser 404: %d", code)
	}
	if code, _ := a.req(t, http.MethodGet, "/sessoes/999999/mapa", "", nil); code != http.StatusNotFound {
		t.Errorf("mapa inexistente deveria ser 404: %d", code)
	}
}

// TestBackoffice_FiltrosELayoutReordenado cobre CA04/CA05.
func TestBackoffice_FiltrosELayoutReordenado(t *testing.T) {
	a := novoAmbiente(t)
	salaA, salaB := a.novaSala(t), a.novaSala(t)
	dia := time.Date(2099, 9, 10, 0, 0, 0, 0, time.UTC)
	sa := sessaoCriada(t, a, 8, salaA, dia.Add(10*time.Hour))
	sb := sessaoCriada(t, a, 8, salaB, dia.Add(10*time.Hour))
	outroDia := sessaoCriada(t, a, 8, salaA, dia.AddDate(0, 0, 1).Add(10*time.Hour))

	ids := func(corpo string) map[int64]bool {
		var l []Sessao
		_ = json.Unmarshal([]byte(corpo), &l)
		m := map[int64]bool{}
		for _, s := range l {
			m[s.ID] = true
		}
		return m
	}
	_, corpo := a.req(t, http.MethodGet, fmt.Sprintf("/backoffice/sessoes?sala_id=%d", salaA), a.operador, nil)
	if m := ids(corpo); !m[sa.ID] || !m[outroDia.ID] || m[sb.ID] {
		t.Errorf("filtro por sala: %v", m)
	}
	_, corpo = a.req(t, http.MethodGet, "/backoffice/sessoes?data=2099-09-10", a.operador, nil)
	if m := ids(corpo); !m[sa.ID] || !m[sb.ID] || m[outroDia.ID] {
		t.Errorf("filtro por dia: %v", m)
	}
	_, corpo = a.req(t, http.MethodGet, fmt.Sprintf("/backoffice/sessoes?data=2099-09-10&sala_id=%d", salaB), a.operador, nil)
	if m := ids(corpo); len(m) != 1 || !m[sb.ID] {
		t.Errorf("filtros combinados: %v", m)
	}
	for _, q := range []string{"?data=10/09/2099", "?sala_id=abc", "?sala_id=0"} {
		if code, _ := a.req(t, http.MethodGet, "/backoffice/sessoes"+q, a.operador, nil); code != http.StatusBadRequest {
			t.Errorf("filtro inválido %s deveria ser 400: %d", q, code)
		}
	}

	// Layout reordenado (salaA tem sessões futuras) não é mudança.
	reordenado := `{"nome":"Reordenada ` + uuid.NewString()[:6] + `","layout":{"fileiras":5,"colunas":8,"pcd":[{"fileira":"A","coluna":1}],"vaos":[{"fileira":"C","coluna":4}]}}`
	if code, corpo := a.req(t, http.MethodPut, fmt.Sprintf("/backoffice/salas/%d", salaA), a.operador, reordenado); code != http.StatusOK {
		t.Errorf("layout igual (outra ordem) deveria ser aceito: %d %s", code, corpo)
	}
}
