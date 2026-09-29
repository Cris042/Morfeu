//go:build integration
// +build integration

package identidade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
	"go.uber.org/zap/zaptest/observer"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/identidade/db"
)

// Suíte do módulo identidade pelas rotas HTTP reais (ADR 0006): um PG (com a
// migration 005 real) e um Redis por pacote; cada teste usa e-mails e
// prefixos de limitador próprios (isolamento sem depender de ordem).
var (
	pool     *pgxpool.Pool
	redisCli *redis.Client
)

var segredoTeste = []byte("segredo-de-teste-identidade-32bytes")

func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "identidade"},
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
		pool, err = pgxpool.New(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%s/identidade?sslmode=disable", host, porta.Port()))
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		defer pool.Close()
		for _, arq := range []string{"../../migrations/005_usuario.up.sql", "../../migrations/006_refresh_token.up.sql"} {
			ddl, err := os.ReadFile(arq)
			if err != nil {
				fmt.Fprintln(os.Stderr, "migration:", err)
				return 1
			}
			if _, err := pool.Exec(ctx, string(ddl)); err != nil {
				fmt.Fprintln(os.Stderr, "aplicar migration:", arq, err)
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

// ambiente é o módulo montado como no main, com limitadores isolados por teste.
type ambiente struct {
	e       *echo.Echo
	servico *Servico
	emissor *autenticacao.Emissor
	logs    *observer.ObservedLogs
}

func novoAmbiente(t *testing.T, concorrencia int) *ambiente {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	emissor, err := autenticacao.NovoEmissor(autenticacao.ConfigJWT{Segredo: segredoTeste, Kid: "k1", TTL: autenticacao.TTLAccessPadrao})
	if err != nil {
		t.Fatalf("emissor: %v", err)
	}
	prefixo := "teste:" + uuid.NewString()[:8] + ":"
	lim := func(nome string, max int, janela time.Duration) *autenticacao.Limitador {
		l, err := autenticacao.NovoLimitador(autenticacao.ConfigLimitador{Redis: redisCli, Prefixo: prefixo + nome + ":", Max: max, Janela: janela}, logger)
		if err != nil {
			t.Fatalf("limitador: %v", err)
		}
		return l
	}
	metricas, err := autenticacao.NovasMetricas()
	if err != nil {
		t.Fatalf("métricas: %v", err)
	}
	s, err := NovoServico(pool, db.New(pool), emissor, Config{
		Argon2:           parametrosRapidos,
		HashConcorrencia: concorrencia,
		LimiteConta:      lim("conta", 5, 5*time.Minute),
		LimiteIP:         lim("ip", 20, 5*time.Minute),
		LimiteRegistro:   lim("registro", 10, time.Hour),
	}, metricas, logger)
	if err != nil {
		t.Fatalf("serviço: %v", err)
	}
	e := echo.New()
	e.IPExtractor = echo.ExtractIPDirect()
	NovoHandler(s, emissor, logger).RegistrarRotas(e)
	return &ambiente{e: e, servico: s, emissor: emissor, logs: logs}
}

type resposta struct {
	status int
	corpo  string
	header http.Header
}

func (a *ambiente) req(t *testing.T, metodo, caminho, ip, token string, corpo any) resposta {
	t.Helper()
	var body *bytes.Reader
	switch c := corpo.(type) {
	case nil:
		body = bytes.NewReader(nil)
	case string:
		body = bytes.NewReader([]byte(c))
	default:
		b, _ := json.Marshal(c)
		body = bytes.NewReader(b)
	}
	r := httptest.NewRequest(metodo, caminho, body)
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	r.RemoteAddr = ip + ":40000"
	if token != "" {
		r.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, r)
	return resposta{status: rec.Code, corpo: strings.TrimSpace(rec.Body.String()), header: rec.Header()}
}

func emailUnico() string { return "u-" + uuid.NewString()[:8] + "@exemplo.com" }

func (a *ambiente) registrar(t *testing.T, email, senha string) resposta {
	t.Helper()
	return a.req(t, http.MethodPost, "/auth/registro", "198.51.100.1", "", map[string]string{"nome": "Usuária Teste", "email": email, "senha": senha})
}

func (a *ambiente) login(t *testing.T, ip, email, senha string) resposta {
	t.Helper()
	return a.req(t, http.MethodPost, "/auth/login", ip, "", map[string]string{"email": email, "senha": senha})
}

// TestRegistro cobre CA01.
func TestRegistro(t *testing.T) {
	a := novoAmbiente(t, 4)
	email := emailUnico()

	r := a.req(t, http.MethodPost, "/auth/registro", "198.51.100.1", "",
		map[string]string{"nome": "Ana", "email": email, "senha": "senha-forte-1", "papel": "operador"})
	if r.status != http.StatusCreated {
		t.Fatalf("registro: %d %s", r.status, r.corpo)
	}
	var u Usuario
	_ = json.Unmarshal([]byte(r.corpo), &u)
	if u.Papel != autenticacao.PapelCliente || u.Email != email || u.ID == uuid.Nil {
		t.Errorf("registro deveria criar cliente com o e-mail normalizado: %+v", u)
	}
	if strings.Contains(r.corpo, "senha") {
		t.Errorf("resposta não pode conter senha/hash: %s", r.corpo)
	}

	dup := a.registrar(t, "  "+strings.ToUpper(email)+" ", "outra-senha-1")
	if dup.status != http.StatusConflict || !strings.Contains(dup.corpo, "email_em_uso") {
		t.Errorf("duplicado com caixa/espaço diferente deveria ser 409: %d %s", dup.status, dup.corpo)
	}

	inval := a.req(t, http.MethodPost, "/auth/registro", "198.51.100.1", "",
		map[string]string{"nome": "", "email": "invalido", "senha": "curta"})
	if inval.status != http.StatusBadRequest || !strings.Contains(inval.corpo, `"campos":["nome","email","senha"]`) {
		t.Errorf("inválido deveria listar os 3 campos: %d %s", inval.status, inval.corpo)
	}
}

// TestLogin_SucessoEEu cobre CA02 e CA06.
func TestLogin_SucessoEEu(t *testing.T) {
	a := novoAmbiente(t, 4)
	email := emailUnico()
	a.registrar(t, email, "senha-forte-1")

	r := a.login(t, "198.51.100.2", strings.ToUpper(email), "senha-forte-1")
	if r.status != http.StatusOK || r.header.Get(echo.HeaderCacheControl) != "no-store" {
		t.Fatalf("login: %d %s %v", r.status, r.corpo, r.header)
	}
	var sessao struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiraEm    int64  `json:"expira_em"`
	}
	_ = json.Unmarshal([]byte(r.corpo), &sessao)
	claims, err := a.emissor.Validar(sessao.AccessToken)
	if err != nil || claims.Papel != autenticacao.PapelCliente || sessao.TokenType != "Bearer" || sessao.ExpiraEm <= 0 || sessao.ExpiraEm > 600 {
		t.Fatalf("token inválido: %v %+v", err, sessao)
	}

	if r := a.req(t, http.MethodGet, "/auth/eu", "198.51.100.2", "", nil); r.status != http.StatusUnauthorized {
		t.Errorf("/auth/eu sem token deveria ser 401, veio %d", r.status)
	}
	eu := a.req(t, http.MethodGet, "/auth/eu", "198.51.100.2", sessao.AccessToken, nil)
	if eu.status != http.StatusOK || !strings.Contains(eu.corpo, email) || !strings.Contains(eu.corpo, `"papel":"cliente"`) {
		t.Errorf("/auth/eu do cliente: %d %s", eu.status, eu.corpo)
	}
}

// TestLogin_ContratoAntiEnumeracao cobre CA03: e-mail inexistente e senha
// errada produzem resposta idêntica e ambos executam o verificador de hash.
func TestLogin_ContratoAntiEnumeracao(t *testing.T) {
	a := novoAmbiente(t, 4)
	email := emailUnico()
	a.registrar(t, email, "senha-forte-1")

	var verificacoes atomic.Int32
	original := a.servico.verificar
	a.servico.verificar = func(senha, hash string) (bool, error) {
		verificacoes.Add(1)
		return original(senha, hash)
	}

	inexistente := a.login(t, "198.51.100.3", emailUnico(), "qualquer-senha")
	errada := a.login(t, "198.51.100.4", email, "senha-errada-1")

	if inexistente.status != http.StatusUnauthorized || inexistente.status != errada.status || inexistente.corpo != errada.corpo {
		t.Fatalf("respostas diferentes: %d %s × %d %s", inexistente.status, inexistente.corpo, errada.status, errada.corpo)
	}
	if inexistente.corpo != `{"erro":"credenciais_invalidas"}` {
		t.Errorf("corpo genérico inesperado: %s", inexistente.corpo)
	}
	for _, h := range []string{echo.HeaderContentType, echo.HeaderWWWAuthenticate, echo.HeaderCacheControl} {
		if inexistente.header.Get(h) != errada.header.Get(h) {
			t.Errorf("header %s difere: %q × %q", h, inexistente.header.Get(h), errada.header.Get(h))
		}
	}
	if verificacoes.Load() != 2 {
		t.Errorf("o hash deve rodar nos dois ramos (inexistente e senha errada); rodou %d vez(es)", verificacoes.Load())
	}
}

// TestLogin_Limites cobre CA04.
func TestLogin_Limites(t *testing.T) {
	t.Run("por conta", func(t *testing.T) {
		a := novoAmbiente(t, 4)
		email := emailUnico()
		a.registrar(t, email, "senha-forte-1")
		for i := 0; i < 5; i++ {
			if r := a.login(t, "198.51.100.10", email, "errada-000"); r.status != http.StatusUnauthorized {
				t.Fatalf("falha %d: %d", i+1, r.status)
			}
		}
		if r := a.login(t, "198.51.100.11", email, "senha-forte-1"); r.status != http.StatusTooManyRequests || !strings.Contains(r.corpo, "muitas_tentativas") {
			t.Errorf("6ª tentativa (mesmo com senha certa e outro IP) deveria ser 429: %d %s", r.status, r.corpo)
		}
	})

	t.Run("sucesso zera a conta", func(t *testing.T) {
		a := novoAmbiente(t, 4)
		email := emailUnico()
		a.registrar(t, email, "senha-forte-1")
		for i := 0; i < 4; i++ {
			a.login(t, "198.51.100.12", email, "errada-000")
		}
		if r := a.login(t, "198.51.100.12", email, "senha-forte-1"); r.status != http.StatusOK {
			t.Fatalf("5ª com senha certa deveria passar: %d", r.status)
		}
		if r := a.login(t, "198.51.100.12", email, "errada-000"); r.status != http.StatusUnauthorized {
			t.Errorf("após sucesso a conta deveria estar zerada (401, não 429): %d", r.status)
		}
	})

	t.Run("por IP", func(t *testing.T) {
		a := novoAmbiente(t, 4)
		ip := "198.51.100.13"
		for i := 0; i < 20; i++ {
			a.login(t, ip, emailUnico(), "errada-000")
		}
		if r := a.login(t, ip, emailUnico(), "errada-000"); r.status != http.StatusTooManyRequests {
			t.Errorf("21ª falha do mesmo IP deveria ser 429: %d", r.status)
		}
		if r := a.login(t, "198.51.100.14", emailUnico(), "errada-000"); r.status != http.StatusUnauthorized {
			t.Errorf("outro IP não pode ser afetado: %d", r.status)
		}
	})
}

// TestLogin_SemaforoSaturado cobre CA05.
func TestLogin_SemaforoSaturado(t *testing.T) {
	a := novoAmbiente(t, 1)
	a.servico.sem <- struct{}{} // única vaga ocupada
	defer func() { <-a.servico.sem }()

	r := a.login(t, "198.51.100.20", emailUnico(), "qualquer-senha")
	if r.status != http.StatusTooManyRequests || !strings.Contains(r.corpo, "tente_novamente") {
		t.Errorf("semáforo cheio deveria dar 429 tente_novamente: %d %s", r.status, r.corpo)
	}
}

// TestSeedOperador cobre CA07 e o papel operador em /auth/eu (CA06).
func TestSeedOperador(t *testing.T) {
	a := novoAmbiente(t, 4)
	email := emailUnico()

	senha, criado, err := SeedOperador(context.Background(), db.New(pool), parametrosRapidos, zap.NewNop(), "Operadora", email)
	if err != nil || !criado || len(senha) != 24 {
		t.Fatalf("seed: criado=%v len=%d err=%v", criado, len(senha), err)
	}
	senha2, criado2, err := SeedOperador(context.Background(), db.New(pool), parametrosRapidos, zap.NewNop(), "Outra", email)
	if err != nil || criado2 || senha2 != "" {
		t.Fatalf("seed deveria ser idempotente: criado=%v err=%v", criado2, err)
	}

	r := a.login(t, "198.51.100.30", email, senha)
	var sessao struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal([]byte(r.corpo), &sessao)
	if eu := a.req(t, http.MethodGet, "/auth/eu", "198.51.100.30", sessao.AccessToken, nil); eu.status != http.StatusOK || !strings.Contains(eu.corpo, `"papel":"operador"`) {
		t.Fatalf("operador do seed deveria autenticar: %d %s", eu.status, eu.corpo)
	}

	for _, rota := range a.e.Routes() {
		permitidas := map[string]bool{"/auth/registro": true, "/auth/login": true, "/auth/refresh": true, "/auth/refresh/logout": true}
		if rota.Method == http.MethodPost && !permitidas[rota.Path] {
			t.Errorf("rota de escrita inesperada no módulo: %s %s", rota.Method, rota.Path)
		}
		if strings.Contains(strings.ToLower(rota.Path), "operador") {
			t.Errorf("nenhuma rota HTTP pode criar operador: %s", rota.Path)
		}
	}
}

// TestSenhaNuncaNosLogs cobre CA08.
func TestSenhaNuncaNosLogs(t *testing.T) {
	a := novoAmbiente(t, 4)
	const senha = "S3nh4-Muito-Secreta!"
	email := emailUnico()

	a.registrar(t, email, senha)
	a.login(t, "198.51.100.40", email, senha+"x")
	a.login(t, "198.51.100.40", email, senha)
	a.req(t, http.MethodPost, "/auth/login", "198.51.100.40", "", `{"email":"`+email+`","senha":"`+senha+`"`) // JSON malformado

	for _, e := range a.logs.All() {
		linha := e.Message
		for k, v := range e.ContextMap() {
			linha += " " + k + "=" + fmt.Sprint(v)
		}
		if strings.Contains(linha, senha) || strings.Contains(linha, email) {
			t.Errorf("log vazou senha ou e-mail: %s", linha)
		}
	}
	if a.logs.Len() == 0 {
		t.Error("esperava logs de auditoria (usuário criado/login) — observer vazio")
	}
}
