//go:build integration
// +build integration

package reserva

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
	"github.com/mclovin137/morfeu/internal/sessao"
	sessaodb "github.com/mclovin137/morfeu/internal/sessao/db"
)

// Suíte do módulo reserva pelas rotas reais (PRD 0015): PG com as migrations
// reais (índice único parcial de verdade), Redis real no limitador e o
// módulo sessao REAL como porta (ADR 0003: o main injeta).
var (
	pool     *pgxpool.Pool
	redisCli *redis.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "reserva"},
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
		cfg, err := pgxpool.ParseConfig(fmt.Sprintf("postgres://postgres:postgres@%s:%s/reserva?sslmode=disable", host, porta.Port()))
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		cfg.MaxConns = 24 // 20 donos disputando + folga: a corrida chega ao banco de fato
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		defer pool.Close()
		for _, arq := range []string{"001_initial_schema.up.sql", "002_outbox_events.up.sql", "007_filmes.up.sql", "008_salas_sessoes.up.sql", "009_holds.up.sql", "010_holds_pedido.up.sql"} {
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

// relogio é o tempo injetado da suíte (ADR 0006): começa em 2098 e as
// sessões são em 2099 — nada depende do relógio real.
type relogio struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relogio) agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relogio) avancar(d time.Duration) {
	r.mu.Lock()
	r.t = r.t.Add(d)
	r.mu.Unlock()
}

type ambiente struct {
	e           *echo.Echo
	rel         *relogio
	sweeper     *Sweeper
	servico     *Servico
	convertidos *atomic.Int64
}

type limites struct{ ip, dono int }

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	return montarAmbiente(t, limites{ip: 100000, dono: 100000})
}

func montarAmbiente(t *testing.T, lim limites) *ambiente {
	t.Helper()
	rel := &relogio{t: time.Date(2098, 1, 1, 12, 0, 0, 0, time.UTC)}
	// zaptest: logs do serviço aparecem na saída do teste que falhar (SQLSTATE).
	logTeste := zaptest.NewLogger(t, zaptest.Level(zapcore.WarnLevel))
	filmes := catalogo.NovoServico(catalogodb.New(pool), pool, nil, zap.NewNop())
	sessoes, err := sessao.NovoServico(sessaodb.New(pool), sessao.Config{Filmes: filmes, Agora: rel.agora}, zap.NewNop())
	if err != nil {
		t.Fatalf("sessao: %v", err)
	}
	prefixo := "teste:" + uuid.NewString() + ":"
	limitador := func(nome string, max int) *autenticacao.Limitador {
		l, err := autenticacao.NovoLimitador(autenticacao.ConfigLimitador{Redis: redisCli, Prefixo: prefixo + nome + ":", Max: max, Janela: time.Minute}, zap.NewNop())
		if err != nil {
			t.Fatalf("limitador: %v", err)
		}
		return l
	}
	convertidos := &atomic.Int64{}
	s, err := NovoServico(pool, Config{
		Sessoes: sessoes, LimiteIP: limitador("ip", lim.ip), LimiteDono: limitador("dono", lim.dono), Agora: rel.agora,
		Cache:    cache.NewRedisCache(redisCli, zap.NewNop()),
		Metricas: Metricas{Convertidos: func(_ context.Context, n int64) { convertidos.Add(n) }},
	}, logTeste)
	if err != nil {
		t.Fatalf("serviço: %v", err)
	}
	e := echo.New()
	NovoHandler(s, logTeste).RegistrarRotas(e)
	return &ambiente{e: e, rel: rel, sweeper: NovoSweeper(pool, Metricas{}, rel.agora, logTeste), servico: s, convertidos: convertidos}
}

// Layout de teste: 3 × 10 com vão em C5 → 29 assentos (A1…C10, sem C5).
const layoutTeste = `{"fileiras":3,"colunas":10,"vaos":[{"fileira":"C","coluna":5}]}`

// novaSessao cria sala + sessão agendada em 2099 direto no banco (o módulo
// sessao já tem a própria suíte; aqui só importa a porta).
func novaSessao(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	var sala, id int64
	if err := pool.QueryRow(ctx, `INSERT INTO salas (nome, layout) VALUES ($1, $2) RETURNING id`,
		"Sala "+uuid.NewString()[:8], layoutTeste).Scan(&sala); err != nil {
		t.Fatalf("sala: %v", err)
	}
	inicio := time.Date(2099, 1, 1, 20, 0, 0, 0, time.UTC)
	if err := pool.QueryRow(ctx, `INSERT INTO sessoes (filme_id, sala_id, inicio, duracao_min, fim, preco_centavos)
		VALUES (1, $1, $2, 100, $3, 3000) RETURNING id`, sala, inicio, inicio.Add(2*time.Hour)).Scan(&id); err != nil {
		t.Fatalf("sessão: %v", err)
	}
	return id
}

func novoDono(t *testing.T) string {
	t.Helper()
	tok, _, err := NovoCarrinho()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type resposta struct {
	code      int
	corpo     string
	cookie    *http.Cookie
	cabecalho http.Header
}

func (a *ambiente) req(metodo, caminho, dono string, corpo any, csrf bool) resposta {
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
	if csrf {
		r.Header.Set(headerAntiCSRF, valorAntiCSRF)
	}
	if dono != "" {
		r.AddCookie(&http.Cookie{Name: cookieCarrinho, Value: dono})
	}
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, r)
	out := resposta{code: rec.Code, corpo: strings.TrimSpace(rec.Body.String()), cabecalho: rec.Header()}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == cookieCarrinho {
			out.cookie = ck
		}
	}
	return out
}

func (a *ambiente) travar(sessaoID int64, dono string, assentos ...string) resposta {
	return a.req(http.MethodPost, fmt.Sprintf("/sessoes/%d/holds", sessaoID), dono, map[string]any{"assentos": assentos}, true)
}

func holdsDe(t *testing.T, r resposta) []holdDTO {
	t.Helper()
	var out struct {
		Holds []holdDTO `json:"holds"`
	}
	if err := json.Unmarshal([]byte(r.corpo), &out); err != nil {
		t.Fatalf("corpo %q: %v", r.corpo, err)
	}
	return out.Holds
}

// vivos conta holds vivos de um assento no instante do relógio da suíte.
func vivos(t *testing.T, sessaoID int64, assento string, agora time.Time) (int, []byte) {
	t.Helper()
	var n int
	var dono []byte
	err := pool.QueryRow(context.Background(), `SELECT count(*), (array_agg(dono_hash))[1] FROM holds
		WHERE sessao_id = $1 AND assento_codigo = $2 AND status = 'ativo' AND expires_at > $3`,
		sessaoID, assento, agora).Scan(&n, &dono)
	if err != nil {
		t.Fatalf("invariante: %v", err)
	}
	return n, dono
}

// disputar solta n donos ao mesmo tempo (barreira) sobre o mesmo lote.
func (a *ambiente) disputar(sessaoID int64, donos []string, assentos ...string) []resposta {
	largada := make(chan struct{})
	var wg sync.WaitGroup
	out := make([]resposta, len(donos))
	for i := range donos {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-largada
			out[i] = a.travar(sessaoID, donos[i], assentos...)
		}(i)
	}
	close(largada)
	wg.Wait()
	return out
}

func novosDonos(t *testing.T, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range out {
		out[i] = novoDono(t)
	}
	return out
}

// exatamenteUm confere 1 × 201 e o resto 409, e que o vivo é do vencedor.
func exatamenteUm(t *testing.T, rodada int, sessaoID int64, assento string, donos []string, rs []resposta, agora time.Time) {
	t.Helper()
	vencedor := -1
	for i, r := range rs {
		switch {
		case r.code == http.StatusCreated && vencedor == -1:
			vencedor = i
		case r.code == http.StatusConflict && strings.Contains(r.corpo, "assento_indisponivel"):
		default:
			t.Fatalf("rodada %d dono %d: %d %s", rodada, i, r.code, r.corpo)
		}
	}
	if vencedor == -1 {
		t.Fatalf("rodada %d: nenhum vencedor", rodada)
	}
	n, dono := vivos(t, sessaoID, assento, agora)
	d, _ := DonoDoToken(donos[vencedor])
	if n != 1 || !bytes.Equal(dono, d.hash) {
		t.Fatalf("rodada %d: %d vivos; dono do vivo confere = %v", rodada, n, bytes.Equal(dono, d.hash))
	}
}

// TestCorridaCanonica cobre CA03: 20 donos, barreira, mesmo assento → 1 vence.
func TestCorridaCanonica(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	for rodada := 1; rodada <= 10; rodada++ {
		assento := fmt.Sprintf("A%d", rodada)
		donos := novosDonos(t, 20)
		rs := a.disputar(sessaoID, donos, assento)
		exatamenteUm(t, rodada, sessaoID, assento, donos, rs, a.rel.agora())
	}
}

// TestRouboDeVencido cobre CA04: vencido é roubado com id novo, também sob corrida.
func TestRouboDeVencido(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	donoA, donoB := novoDono(t), novoDono(t)
	r := a.travar(sessaoID, donoA, "B1")
	if r.code != http.StatusCreated {
		t.Fatalf("A trava: %d %s", r.code, r.corpo)
	}
	idA := holdsDe(t, r)[0].ID
	if r := a.travar(sessaoID, donoB, "B1"); r.code != http.StatusConflict {
		t.Fatalf("B com A vivo: %d", r.code)
	}
	a.rel.avancar(TTLHold) // prazo exato: já roubável (RN02)
	r = a.travar(sessaoID, donoB, "B1")
	if r.code != http.StatusCreated || holdsDe(t, r)[0].ID == idA {
		t.Fatalf("roubo: %d %s", r.code, r.corpo)
	}
	if r := a.req(http.MethodGet, "/holds", donoA, nil, false); r.code != 200 || r.corpo != "[]" {
		t.Fatalf("A ainda vê hold: %s", r.corpo)
	}
	if r := a.req(http.MethodDelete, "/holds/"+idA.String(), donoA, nil, true); r.code != http.StatusNotFound {
		t.Fatalf("id antigo: %d", r.code)
	}

	for rodada := 1; rodada <= 10; rodada++ {
		assento := fmt.Sprintf("A%d", rodada)
		if r := a.travar(sessaoID, novoDono(t), assento); r.code != http.StatusCreated {
			t.Fatalf("preparo: %d %s", r.code, r.corpo)
		}
		a.rel.avancar(TTLHold + time.Minute)
		donos := novosDonos(t, 20)
		rs := a.disputar(sessaoID, donos, assento)
		exatamenteUm(t, rodada, sessaoID, assento, donos, rs, a.rel.agora())
	}
}

// TestLoteTudoOuNada cobre CA05.
func TestLoteTudoOuNada(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	donoA, donoB := novoDono(t), novoDono(t)
	if r := a.travar(sessaoID, donoA, "A8"); r.code != http.StatusCreated {
		t.Fatalf("A: %d", r.code)
	}
	r := a.travar(sessaoID, donoB, "A7", "A8")
	if r.code != http.StatusConflict || !strings.Contains(r.corpo, `"assentos":["A8"]`) {
		t.Fatalf("lote parcial: %d %s", r.code, r.corpo)
	}
	if n, _ := vivos(t, sessaoID, "A7", a.rel.agora()); n != 0 {
		t.Fatal("A7 ficou travado: hold órfão")
	}

	// Lotes cruzados concorrentes: a ordem global impede espera circular.
	for rodada := 1; rodada <= 10; rodada++ {
		x, y := fmt.Sprintf("B%d", rodada), fmt.Sprintf("C%d", rodada)
		if rodada == 5 {
			y = "C10" // C5 é vão
		}
		d1, d2 := novoDono(t), novoDono(t)
		largada := make(chan struct{})
		var wg sync.WaitGroup
		rs := make([]resposta, 2)
		for i, lote := range [][]string{{x, y}, {y, x}} {
			wg.Add(1)
			go func(i int, dono string, lote []string) {
				defer wg.Done()
				<-largada
				rs[i] = a.travar(sessaoID, dono, lote...)
			}(i, []string{d1, d2}[i], lote)
		}
		close(largada)
		wg.Wait()
		for i, r := range rs {
			if r.code != http.StatusCreated && r.code != http.StatusConflict {
				t.Fatalf("rodada %d lote %d: %d %s", rodada, i, r.code, r.corpo)
			}
		}
		nx, dx := vivos(t, sessaoID, x, a.rel.agora())
		ny, dy := vivos(t, sessaoID, y, a.rel.agora())
		if nx != 1 || ny != 1 || !bytes.Equal(dx, dy) {
			t.Fatalf("rodada %d: lote dividido entre donos (%d/%d)", rodada, nx, ny)
		}
		a.rel.avancar(TTLHold) // libera C10 para a próxima rodada que o use
	}
}

// TestTetoPorDono cobre CA06.
func TestTetoPorDono(t *testing.T) {
	a := novoAmbiente(t)
	s1, s2 := novaSessao(t), novaSessao(t)
	dono := novoDono(t)
	if r := a.travar(s1, dono, "A1", "A2", "A3", "A4"); r.code != http.StatusCreated {
		t.Fatalf("4: %d", r.code)
	}
	r := a.travar(s1, dono, "A1") // próprio: idempotente, mesmo id
	if r.code != http.StatusCreated {
		t.Fatalf("repetir próprio: %d %s", r.code, r.corpo)
	}
	if r := a.travar(s2, dono, "A1", "A2"); r.code != http.StatusCreated {
		t.Fatalf("5–6 em outra sessão: %d", r.code)
	}
	if r := a.travar(s2, dono, "A3"); r.code != http.StatusConflict || !strings.Contains(r.corpo, "limite_holds") {
		t.Fatalf("7º: %d %s", r.code, r.corpo)
	}

	// Paralelo do mesmo dono: o advisory lock serializa a contagem.
	for rodada := 1; rodada <= 5; rodada++ {
		s := novaSessao(t)
		d := novoDono(t)
		largada := make(chan struct{})
		var wg sync.WaitGroup
		rs := make([]resposta, 2)
		for i, lote := range [][]string{{"A1", "A2", "A3", "A4"}, {"B1", "B2", "B3", "B4"}} {
			wg.Add(1)
			go func(i int, lote []string) {
				defer wg.Done()
				<-largada
				rs[i] = a.travar(s, d, lote...)
			}(i, lote)
		}
		close(largada)
		wg.Wait()
		ok, limite := 0, 0
		for _, r := range rs {
			switch {
			case r.code == http.StatusCreated:
				ok++
			case r.code == http.StatusConflict && strings.Contains(r.corpo, "limite_holds"):
				limite++
			}
		}
		if ok != 1 || limite != 1 {
			t.Fatalf("rodada %d: %d/%d — %v", rodada, rs[0].code, rs[1].code, rs)
		}
	}
}

// TestExtensaoELiberacao cobre CA07.
func TestExtensaoELiberacao(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	dono, outro := novoDono(t), novoDono(t)
	h := holdsDe(t, a.travar(sessaoID, dono, "A1"))[0]
	caminho := "/holds/" + h.ID.String()

	if r := a.req(http.MethodPost, caminho+"/estender", outro, nil, true); r.code != http.StatusNotFound {
		t.Fatalf("estender alheio: %d", r.code)
	}
	r := a.req(http.MethodPost, caminho+"/estender", dono, nil, true)
	var e holdDTO
	_ = json.Unmarshal([]byte(r.corpo), &e)
	if r.code != http.StatusOK || !e.ExpiraEm.Equal(h.ExpiraEm.Add(ExtensaoHold)) || e.ExtensoesUsadas != 1 {
		t.Fatalf("1ª extensão: %d %s", r.code, r.corpo)
	}
	if r := a.req(http.MethodPost, caminho+"/estender", dono, nil, true); r.code != http.StatusConflict || !strings.Contains(r.corpo, "extensao_esgotada") {
		t.Fatalf("2ª extensão: %d %s", r.code, r.corpo)
	}
	if r := a.req(http.MethodDelete, caminho, outro, nil, true); r.code != http.StatusNotFound {
		t.Fatalf("liberar alheio: %d", r.code)
	}
	if r := a.req(http.MethodDelete, caminho, dono, nil, true); r.code != http.StatusNoContent {
		t.Fatalf("liberar: %d", r.code)
	}
	if r := a.req(http.MethodDelete, caminho, dono, nil, true); r.code != http.StatusNotFound {
		t.Fatalf("liberar de novo: %d", r.code)
	}
	if r := a.travar(sessaoID, outro, "A1"); r.code != http.StatusCreated {
		t.Fatalf("liberado não voltou: %d", r.code)
	}

	// Vencido não se estende (404), mesmo sem o sweeper ter passado.
	h2 := holdsDe(t, a.travar(sessaoID, dono, "A2"))[0]
	a.rel.avancar(TTLHold)
	if r := a.req(http.MethodPost, "/holds/"+h2.ID.String()+"/estender", dono, nil, true); r.code != http.StatusNotFound {
		t.Fatalf("estender vencido: %d", r.code)
	}
}

// TestSeguranca cobre CA08.
func TestSeguranca(t *testing.T) {
	a := montarAmbiente(t, limites{ip: 30, dono: 20})
	sessaoID := novaSessao(t)

	if r := a.req(http.MethodPost, fmt.Sprintf("/sessoes/%d/holds", sessaoID), "", map[string]any{"assentos": []string{"A1"}}, false); r.code != http.StatusForbidden {
		t.Fatalf("sem anti-CSRF: %d", r.code)
	}
	r := a.travar(sessaoID, "", "A1")
	if r.code != http.StatusCreated || r.cookie == nil {
		t.Fatalf("1ª trava sem carrinho: %d %s", r.code, r.corpo)
	}
	ck := r.cookie
	if !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteStrictMode || ck.Path != "/" {
		t.Errorf("atributos do cookie: %+v", ck)
	}
	if _, ok := DonoDoToken(ck.Value); !ok {
		t.Error("token do cookie fora do formato")
	}
	if strings.Contains(r.corpo, ck.Value) || strings.Contains(r.corpo, "dono") {
		t.Errorf("resposta vaza o dono: %s", r.corpo)
	}
	if r := a.travar(sessaoID, ck.Value, "A2"); r.cookie != nil {
		t.Error("carrinho válido não deve ser reemitido")
	}
	if r := a.req(http.MethodGet, "/holds", ck.Value, nil, false); !strings.Contains(r.corpo, `"A1"`) || !strings.Contains(r.corpo, `"A2"`) {
		t.Errorf("meus holds: %s", r.corpo)
	}
	if r := a.req(http.MethodGet, "/holds", novoDono(t), nil, false); r.corpo != "[]" {
		t.Errorf("outro dono vê holds: %s", r.corpo)
	}
	var hash []byte
	_ = pool.QueryRow(context.Background(), `SELECT dono_hash FROM holds WHERE sessao_id = $1 LIMIT 1`, sessaoID).Scan(&hash)
	if bytes.Contains(hash, []byte(ck.Value)) || len(hash) != 32 {
		t.Error("token em claro no banco")
	}

	// 2 requisições já contaram; a 31ª do mesmo IP no minuto → 429.
	for i := 3; i <= 30; i++ {
		if r := a.travar(sessaoID, novoDono(t), "Z1"); r.code == http.StatusTooManyRequests {
			t.Fatalf("429 cedo na %dª", i)
		}
	}
	if r := a.travar(sessaoID, novoDono(t), "A3"); r.code != http.StatusTooManyRequests {
		t.Fatalf("31ª: %d", r.code)
	}
}

// TestPorta cobre CA09: sessão indisponível → 404; assento fora do layout → 400.
func TestPorta(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	dono := novoDono(t)
	for _, c := range []struct {
		nome     string
		sessao   int64
		assentos []string
		code     int
	}{
		{"inexistente", 999999, []string{"A1"}, 404},
		{"vão", sessaoID, []string{"C5"}, 400},
		{"além da grade", sessaoID, []string{"D1"}, 400},
		{"coluna além", sessaoID, []string{"A11"}, 400},
		{"formato", sessaoID, []string{"a1"}, 400},
		{"vazio", sessaoID, []string{}, 400},
	} {
		if r := a.travar(c.sessao, dono, c.assentos...); r.code != c.code {
			t.Errorf("%s: %d %s", c.nome, r.code, r.corpo)
		}
	}
	if r := a.req(http.MethodPost, fmt.Sprintf("/sessoes/%d/holds", sessaoID), dono, `{"assentos":["A1"],"x":1}`, true); r.code != 400 {
		t.Errorf("campo desconhecido: %d", r.code)
	}
	cancelada := novaSessao(t)
	_, _ = pool.Exec(context.Background(), `UPDATE sessoes SET status = 'cancelada' WHERE id = $1`, cancelada)
	if r := a.travar(cancelada, dono, "A1"); r.code != 404 {
		t.Errorf("cancelada: %d", r.code)
	}
	a.rel.avancar(time.Date(2099, 1, 1, 20, 0, 0, 0, time.UTC).Sub(a.rel.agora())) // sessão começa
	if r := a.travar(sessaoID, dono, "A1"); r.code != 404 {
		t.Errorf("iniciada: %d", r.code)
	}
}

// TestSweeper cobre CA10.
func TestSweeper(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	ctx := context.Background()
	vencido := holdsDe(t, a.travar(sessaoID, novoDono(t), "A1"))[0]
	a.rel.avancar(TTLHold)
	vivo := holdsDe(t, a.travar(sessaoID, novoDono(t), "A2"))[0]
	if _, err := a.sweeper.VarrerExpirados(ctx); err != nil {
		t.Fatal(err)
	}
	status := func(id uuid.UUID) string {
		var s string
		_ = pool.QueryRow(ctx, `SELECT status FROM holds WHERE id = $1`, id).Scan(&s)
		return s
	}
	if status(vencido.ID) != "expirado" || status(vivo.ID) != "ativo" {
		t.Fatalf("status: vencido=%s vivo=%s", status(vencido.ID), status(vivo.ID))
	}

	// Corrida sweeper × roubo do mesmo assento vencido.
	for rodada := 1; rodada <= 10; rodada++ {
		assento := fmt.Sprintf("B%d", rodada)
		_ = a.travar(sessaoID, novoDono(t), assento)
		a.rel.avancar(TTLHold)
		dono := novoDono(t)
		largada := make(chan struct{})
		var wg sync.WaitGroup
		var r resposta
		var errS error
		wg.Add(2)
		go func() { defer wg.Done(); <-largada; r = a.travar(sessaoID, dono, assento) }()
		go func() { defer wg.Done(); <-largada; _, errS = a.sweeper.VarrerExpirados(ctx) }()
		close(largada)
		wg.Wait()
		if errS != nil || r.code != http.StatusCreated {
			t.Fatalf("rodada %d: sweeper=%v trava=%d %s", rodada, errS, r.code, r.corpo)
		}
		if n, _ := vivos(t, sessaoID, assento, a.rel.agora()); n != 1 {
			t.Fatalf("rodada %d: %d vivos", rodada, n)
		}
	}
}

// ocupacao lê a ocupação pública e devolve os códigos ocupados.
func (a *ambiente) ocupacao(t *testing.T, sessaoID int64) (resposta, []string) {
	t.Helper()
	r := a.req(http.MethodGet, fmt.Sprintf("/sessoes/%d/ocupacao", sessaoID), "", nil, false)
	var o Ocupacao
	if r.code == http.StatusOK {
		if err := json.Unmarshal([]byte(r.corpo), &o); err != nil {
			t.Fatalf("corpo %q: %v", r.corpo, err)
		}
	}
	return r, o.Ocupados
}

// expirarCache simula o fim do TTL de 3 s sem esperar (ADR 0006: sem sleep).
func expirarCache(t *testing.T, sessaoID int64) {
	t.Helper()
	if err := redisCli.Del(context.Background(), chaveOcupacao(sessaoID)).Err(); err != nil {
		t.Fatal(err)
	}
}

// TestOcupacao cobre CA01–CA05 do PRD 0016.
func TestOcupacao(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	dono := novoDono(t)
	liberado := holdsDe(t, a.travar(sessaoID, dono, "A2", "B7"))
	if r := a.req(http.MethodDelete, "/holds/"+liberado[0].ID.String(), dono, nil, true); r.code != http.StatusNoContent {
		t.Fatalf("liberar: %d", r.code)
	}

	r, ocupados := a.ocupacao(t, sessaoID)
	if r.code != http.StatusOK || strings.Join(ocupados, ",") != "B7" {
		t.Fatalf("ocupação: %d %s", r.code, r.corpo)
	}
	var campos map[string]any
	_ = json.Unmarshal([]byte(r.corpo), &campos)
	if len(campos) != 2 || strings.Contains(r.corpo, liberado[1].ID.String()) || strings.Contains(r.corpo, "expira") {
		t.Errorf("contrato vaza dados do hold: %s", r.corpo)
	}
	ttl, err := redisCli.PTTL(context.Background(), chaveOcupacao(sessaoID)).Result()
	if err != nil || ttl <= 0 || ttl > ttlOcupacao {
		t.Fatalf("TTL do cache = %v (%v)", ttl, err)
	}

	// Com o cache valendo, um hold novo ainda não aparece (sem invalidação ativa).
	if r := a.travar(sessaoID, novoDono(t), "A1"); r.code != http.StatusCreated {
		t.Fatalf("trava: %d", r.code)
	}
	if _, ocupados := a.ocupacao(t, sessaoID); strings.Join(ocupados, ",") != "B7" {
		t.Fatalf("cache ignorado: %v", ocupados)
	}
	expirarCache(t, sessaoID)
	if _, ocupados := a.ocupacao(t, sessaoID); strings.Join(ocupados, ",") != "A1,B7" {
		t.Fatalf("depois do TTL: %v", ocupados)
	}

	// Vencido (sweeper ainda não passou) conta como livre.
	a.rel.avancar(TTLHold)
	expirarCache(t, sessaoID)
	if r, ocupados := a.ocupacao(t, sessaoID); r.corpo != `{"sessao_id":`+fmt.Sprint(sessaoID)+`,"ocupados":[]}` || len(ocupados) != 0 {
		t.Fatalf("vencidos contados: %s", r.corpo)
	}
}

// TestOcupacao_SessaoIndisponivel cobre CA04 e CA05.
func TestOcupacao_SessaoIndisponivel(t *testing.T) {
	a := novoAmbiente(t)
	aberta := novaSessao(t)
	r := a.req(http.MethodGet, fmt.Sprintf("/sessoes/%d/ocupacao", aberta), "", nil, false)
	if r.code != http.StatusOK || r.cabecalho.Get(echo.HeaderCacheControl) != "public, max-age=2" {
		t.Fatalf("aberta: %d, Cache-Control %q", r.code, r.cabecalho.Get(echo.HeaderCacheControl))
	}
	cancelada := novaSessao(t)
	_, _ = pool.Exec(context.Background(), `UPDATE sessoes SET status = 'cancelada' WHERE id = $1`, cancelada)
	for nome, id := range map[string]int64{"inexistente": 999999, "cancelada": cancelada} {
		if r, _ := a.ocupacao(t, id); r.code != http.StatusNotFound {
			t.Errorf("%s: %d", nome, r.code)
		}
		if n, _ := redisCli.Exists(context.Background(), chaveOcupacao(id)).Result(); n != 0 {
			t.Errorf("%s: 404 foi cacheado", nome)
		}
	}
	iniciada := novaSessao(t)
	a.rel.avancar(time.Date(2099, 1, 1, 20, 0, 0, 0, time.UTC).Sub(a.rel.agora()))
	if r, _ := a.ocupacao(t, iniciada); r.code != http.StatusNotFound {
		t.Errorf("iniciada: %d", r.code)
	}
}
