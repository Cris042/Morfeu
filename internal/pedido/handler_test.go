//go:build integration
// +build integration

package pedido

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/catalogo"
	catalogodb "github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/pedido/db"
	"github.com/mclovin137/morfeu/internal/pedido/pagamento"
	"github.com/mclovin137/morfeu/internal/reserva"
	"github.com/mclovin137/morfeu/internal/sessao"
	sessaodb "github.com/mclovin137/morfeu/internal/sessao/db"
)

// Suíte do módulo pedido pelas rotas reais (PRD 0023): PG com as migrations
// reais, Redis real nos limitadores, sessao e reserva REAIS ligados pelo
// mesmo adapter do main (ADR 0010) e o gateway fake programável.
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
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "pedido"},
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
		cfg, err := pgxpool.ParseConfig(fmt.Sprintf("postgres://postgres:postgres@%s:%s/pedido?sslmode=disable", host, porta.Port()))
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		cfg.MaxConns = 24
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		defer pool.Close()
		for _, arq := range []string{"001_initial_schema.up.sql", "002_outbox_events.up.sql", "007_filmes.up.sql",
			"008_salas_sessoes.up.sql", "009_holds.up.sql", "010_holds_pedido.up.sql", "011_pedidos.up.sql",
			"012_stripe_eventos.up.sql", "013_pedidos_tarefas.up.sql", "014_pedidos_cobranca_encerrada.up.sql", "015_pedidos_usuario.up.sql"} {
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

// relogio é o tempo injetado (ADR 0006): começa em 2098; sessões em 2099.
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

// reservaAdapter espelha o adapter do main (cmd/morfeu reservaDoPedido).
type reservaAdapter struct{ s *reserva.Servico }

func (a reservaAdapter) DonoDoToken(token string) ([]byte, bool) {
	d, ok := reserva.DonoDoToken(token)
	if !ok {
		return nil, false
	}
	return d.Hash(), true
}

func (a reservaAdapter) PrenderParaPedido(ctx context.Context, tx outbox.Tx, h []byte, sessaoID int64, codigos []string, id uuid.UUID, ate time.Time) error {
	d, ok := reserva.DonoDoHash(h)
	if !ok {
		return ErrHoldsInvalidos
	}
	err := a.s.PrenderParaPedido(ctx, tx, d, sessaoID, codigos, id, ate)
	if errors.Is(err, reserva.ErrHoldsDoPedido) || errors.Is(err, reserva.ErrDadosInvalidos) {
		return ErrHoldsInvalidos
	}
	return err
}

func (a reservaAdapter) LiberarDoPedido(ctx context.Context, tx outbox.Tx, id uuid.UUID) (int64, error) {
	return a.s.LiberarDoPedido(ctx, tx, id)
}

func (a reservaAdapter) ConverterDoPedido(ctx context.Context, tx outbox.Tx, id uuid.UUID) ([]string, error) {
	return a.s.ConverterDoPedido(ctx, tx, id)
}

type ambiente struct {
	e       *echo.Echo
	rel     *relogio
	reserva *reserva.Servico
	servico *Servico
	gateway *pagamento.Fake
	funil   *sync.Map
	compens *sync.Map
	emissor *autenticacao.Emissor
}

type limites struct{ ip, dono int }

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	return montarAmbiente(t, limites{ip: 100000, dono: 100000})
}

func montarAmbiente(t *testing.T, lim limites) *ambiente {
	t.Helper()
	rel := &relogio{t: time.Date(2098, 1, 1, 12, 0, 0, 0, time.UTC)}
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
	res, err := reserva.NovoServico(pool, reserva.Config{
		Sessoes: sessoes, LimiteIP: limitador("r-ip", 100000), LimiteDono: limitador("r-dono", 100000), Agora: rel.agora,
	}, logTeste)
	if err != nil {
		t.Fatalf("reserva: %v", err)
	}
	gw := pagamento.NovoFake()
	funil, compens := &sync.Map{}, &sync.Map{}
	contar := func(m *sync.Map) func(context.Context, string) {
		var mu sync.Mutex
		return func(_ context.Context, chave string) {
			mu.Lock()
			defer mu.Unlock()
			n, _ := m.LoadOrStore(chave, new(int))
			*(n.(*int))++
		}
	}
	wh, err := pagamento.NovoWebhook(segredoWebhookTeste)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NovoServico(pool, Config{
		Sessoes: sessoes, Reserva: reservaAdapter{res}, Gateway: gw,
		LimiteIP: limitador("ip", lim.ip), LimiteDono: limitador("dono", lim.dono), Agora: rel.agora,
		Webhook: wh, LimiteWebhook: limitador("webhook", 100000),
		SegredosToken: map[int16][]byte{1: segredoTokenTeste},
		Funil:         contar(funil),
		Compensacao:   contar(compens),
	}, logTeste)
	if err != nil {
		t.Fatalf("serviço: %v", err)
	}
	e := echo.New()
	emissor, err := autenticacao.NovoEmissor(autenticacao.ConfigJWT{Segredo: []byte("segredo-jwt-de-teste-com-32-bytes!!"), Kid: "k1", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	NovoHandler(s, logTeste).
		ComConta(autenticacao.Opcional(emissor), autenticacao.Exigir(emissor, autenticacao.PapelCliente, autenticacao.PapelOperador), autenticacao.UsuarioID).
		ComRotasDeTeste().RegistrarRotas(e)
	return &ambiente{e: e, rel: rel, reserva: res, servico: s, gateway: gw, funil: funil, compens: compens, emissor: emissor}
}

// segredoTokenTeste é o segredo do HMAC do ingresso na suíte (32 bytes).
var segredoTokenTeste = []byte("segredo-de-teste-do-token-32byte")

// segredoWebhookTeste assina os eventos gerados nos testes (sem rede).
const segredoWebhookTeste = "whsec_teste_da_suite"

const layoutTeste = `{"fileiras":3,"colunas":10,"vaos":[]}`

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

// carrinho cria um carrinho com os assentos travados na sessão.
func (a *ambiente) carrinho(t *testing.T, sessaoID int64, assentos ...string) string {
	t.Helper()
	tok, d, err := reserva.NovoCarrinho()
	if err != nil {
		t.Fatal(err)
	}
	if len(assentos) > 0 {
		if _, err := a.reserva.Travar(context.Background(), sessaoID, assentos, d); err != nil {
			t.Fatalf("travar %v: %v", assentos, err)
		}
	}
	return tok
}

type resposta struct {
	code      int
	corpo     string
	cabecalho http.Header
}

func (a *ambiente) req(metodo, caminho, carrinho string, corpo any, csrf bool) resposta {
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
	if carrinho != "" {
		r.AddCookie(&http.Cookie{Name: cookieCarrinho, Value: carrinho}) //nolint:gosec // cookie de requisição no teste
	}
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, r)
	return resposta{code: rec.Code, corpo: strings.TrimSpace(rec.Body.String()), cabecalho: rec.Header()}
}

func (a *ambiente) criar(carrinho string, sessaoID int64, assentos ...string) resposta {
	return a.req(http.MethodPost, "/pedidos", carrinho, map[string]any{"email": "ana@exemplo.com", "sessao_id": sessaoID, "assentos": assentos}, true)
}

type criadoDTO struct {
	Pedido       pedidoDTO `json:"pedido"`
	ClientSecret string    `json:"client_secret"`
}

func criadoDe(t *testing.T, r resposta) criadoDTO {
	t.Helper()
	var out criadoDTO
	if r.code != http.StatusCreated || json.Unmarshal([]byte(r.corpo), &out) != nil {
		t.Fatalf("criar: %d %s", r.code, r.corpo)
	}
	return out
}

func statusDe(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM pedidos WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func trilha(t *testing.T, id uuid.UUID) string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT coalesce(de, '-') || '>' || para FROM pedido_eventos WHERE pedido_id = $1 ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func (a *ambiente) etapa(nome string) int { return lerContador(a.funil, nome) }

func (a *ambiente) compensacoes(passo string) int { return lerContador(a.compens, passo) }

func lerContador(m *sync.Map, chave string) int {
	if n, ok := m.Load(chave); ok {
		return *(n.(*int))
	}
	return 0
}

// TestCriar_CaminhoFeliz cobre CA01, CA02 e CA09.
func TestCriar_CaminhoFeliz(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID, "A1", "A2")
	rc := a.criar(car, sessaoID, "A2", "A1")
	if rc.cabecalho.Get(echo.HeaderCacheControl) != "no-store" {
		t.Fatalf("Cache-Control: %q", rc.cabecalho.Get(echo.HeaderCacheControl))
	}
	c := criadoDe(t, rc)
	p := c.Pedido
	if p.TotalCentavos != 6000 || p.Status != AguardandoPagamento || !p.ExpiraEm.Equal(a.rel.agora().Add(TTLPedido)) ||
		c.ClientSecret == "" || len(p.Codigo) != 16 || strings.Contains(c.ClientSecret, "ana@") {
		t.Fatalf("resposta: %+v", c)
	}
	chamadas := a.gateway.Chamadas()
	if len(chamadas) != 1 || chamadas[0].ValorCentavos != 6000 || chamadas[0].Moeda != "brl" ||
		chamadas[0].PedidoID != p.ID || chamadas[0].ChaveIdempotencia != "pedido-"+p.ID.String()+"-cobranca" {
		t.Fatalf("cobrança: %+v", chamadas)
	}
	var pi *string
	var presos int
	ctx := context.Background()
	_ = pool.QueryRow(ctx, `SELECT payment_intent_id FROM pedidos WHERE id = $1`, p.ID).Scan(&pi)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM holds WHERE pedido_id = $1 AND status = 'ativo' AND expires_at = $2`,
		p.ID, a.rel.agora().Add(TTLPedido+MargemHold)).Scan(&presos)
	if pi == nil || *pi != "pi_fake_"+p.ID.String() || presos != 2 {
		t.Fatalf("persistência: pi=%v presos=%d", pi, presos)
	}
	if got := trilha(t, p.ID); got != "->aguardando_pagamento" {
		t.Fatalf("trilha: %s", got)
	}
	if a.etapa(EtapaPedidoCriado) != 1 || a.etapa(EtapaCobrancaCriada) != 1 {
		t.Fatalf("funil: criado=%d cobrança=%d", a.etapa(EtapaPedidoCriado), a.etapa(EtapaCobrancaCriada))
	}

	// CA06: o dono vê o pedido (sem e-mail); outro carrinho e id inválido → 404.
	r := a.req(http.MethodGet, "/pedidos/"+p.ID.String(), car, nil, false)
	if r.code != http.StatusOK || strings.Contains(r.corpo, "ana@") || !strings.Contains(r.corpo, `"total_centavos":6000`) {
		t.Fatalf("obter: %d %s", r.code, r.corpo)
	}
	for nome, rr := range map[string]resposta{
		"outro carrinho": a.req(http.MethodGet, "/pedidos/"+p.ID.String(), a.carrinho(t, sessaoID), nil, false),
		"sem carrinho":   a.req(http.MethodGet, "/pedidos/"+p.ID.String(), "", nil, false),
		"id inválido":    a.req(http.MethodGet, "/pedidos/abc", car, nil, false),
	} {
		if rr.code != http.StatusNotFound {
			t.Errorf("%s: %d", nome, rr.code)
		}
	}
}

// TestCriar_Recusas cobre CA03, CA04 e CA08.
func TestCriar_Recusas(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID, "B1")
	_ = a.carrinho(t, sessaoID, "B2") // B2 fica com outro carrinho
	semLinhas := func(nome string) {
		t.Helper()
		var n int
		_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM pedidos WHERE sessao_id = $1`, sessaoID).Scan(&n)
		if n != 0 {
			t.Fatalf("%s: sobrou pedido (%d)", nome, n)
		}
	}
	casos := []struct {
		nome   string
		r      resposta
		code   int
		trecho string
	}{
		{"total do cliente", a.req(http.MethodPost, "/pedidos", car, `{"email":"ana@exemplo.com","sessao_id":`+fmt.Sprint(sessaoID)+`,"assentos":["B1"],"total_centavos":1}`, true), 400, "requisicao_invalida"},
		{"e-mail inválido", a.req(http.MethodPost, "/pedidos", car, map[string]any{"email": "x", "sessao_id": sessaoID, "assentos": []string{"B1"}}, true), 400, `"email"`},
		{"sem csrf", a.req(http.MethodPost, "/pedidos", car, map[string]any{"email": "ana@exemplo.com", "sessao_id": sessaoID, "assentos": []string{"B1"}}, false), 403, "csrf"},
		{"sem carrinho", a.criar("", sessaoID, "B1"), 409, "holds_invalidos"},
		{"carrinho forjado", a.criar("forjado", sessaoID, "B1"), 409, "holds_invalidos"},
		{"assento de outro", a.criar(car, sessaoID, "B1", "B2"), 409, "holds_invalidos"},
		{"assento sem hold", a.criar(car, sessaoID, "B3"), 409, "holds_invalidos"},
		{"sessão inexistente", a.criar(car, 999999, "B1"), 404, "nao_encontrado"},
	}
	for _, c := range casos {
		if c.r.code != c.code || !strings.Contains(c.r.corpo, c.trecho) {
			t.Errorf("%s: %d %s", c.nome, c.r.code, c.r.corpo)
		}
	}
	semLinhas("recusas")
	if len(a.gateway.Chamadas()) != 0 {
		t.Fatal("recusa não pode chegar ao gateway")
	}
}

// TestCriar_Pendente cobre CA05: 1 pedido pendente por carrinho; vencido
// expira de forma lazy, devolve os assentos e libera o carrinho.
func TestCriar_Pendente(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID, "C1")
	primeiro := criadoDe(t, a.criar(car, sessaoID, "C1")).Pedido
	r := a.criar(car, sessaoID, "C1")
	if r.code != http.StatusConflict || !strings.Contains(r.corpo, `"pedido_pendente"`) || !strings.Contains(r.corpo, primeiro.ID.String()) {
		t.Fatalf("pendente: %d %s", r.code, r.corpo)
	}

	// Depois do prazo: a criação expira o pendente e devolve o assento; como
	// o hold era do pedido vencido, o cliente precisa travar de novo.
	a.rel.avancar(TTLPedido)
	if r := a.criar(car, sessaoID, "C1"); r.code != http.StatusConflict || !strings.Contains(r.corpo, "holds_invalidos") {
		t.Fatalf("após o prazo sem trava nova: %d %s", r.code, r.corpo)
	}
	if s := statusDe(t, primeiro.ID); s != string(Expirado) {
		t.Fatalf("pendente vencido: %s", s)
	}
	if got := trilha(t, primeiro.ID); got != "->aguardando_pagamento aguardando_pagamento>expirado" {
		t.Fatalf("trilha: %s", got)
	}
	d, _ := reserva.DonoDoToken(car)
	if _, err := a.reserva.Travar(context.Background(), sessaoID, []string{"C1"}, d); err != nil {
		t.Fatalf("assento não voltou: %v", err)
	}
	segundo := criadoDe(t, a.criar(car, sessaoID, "C1")).Pedido
	if segundo.ID == primeiro.ID {
		t.Fatal("deveria ser um pedido novo")
	}
}

// TestCriar_GatewayFalha cobre CA07: 503 + Retry-After, pedido falhou, holds
// devolvidos e o assento volta à ocupação livre.
func TestCriar_GatewayFalha(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID, "A5", "A6")
	a.gateway.FalharNa(1)
	r := a.criar(car, sessaoID, "A5", "A6")
	if r.code != http.StatusServiceUnavailable || r.cabecalho.Get("Retry-After") == "" || !strings.Contains(r.corpo, "pagamento_indisponivel") {
		t.Fatalf("gateway fora: %d %s", r.code, r.corpo)
	}
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM pedidos WHERE sessao_id = $1`, sessaoID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if s := statusDe(t, id); s != string(Falhou) {
		t.Fatalf("status: %s", s)
	}
	if got := trilha(t, id); got != "->aguardando_pagamento aguardando_pagamento>falhou" {
		t.Fatalf("trilha: %s", got)
	}
	o, err := a.reserva.Ocupacao(context.Background(), sessaoID)
	if err != nil || len(o.Ocupados) != 0 {
		t.Fatalf("ocupação após desfazer: %+v %v", o, err)
	}
	if a.etapa(EtapaGatewayFalhou) != 1 || a.etapa(EtapaCobrancaCriada) != 0 || a.compensacoes(PassoCobranca) != 1 {
		t.Fatal("funil/compensação do gateway fora")
	}
	// O carrinho não fica bloqueado: trava de novo e compra.
	d, _ := reserva.DonoDoToken(car)
	if _, err := a.reserva.Travar(context.Background(), sessaoID, []string{"A5"}, d); err != nil {
		t.Fatal(err)
	}
	criadoDe(t, a.criar(car, sessaoID, "A5"))
}

// TestCriar_ConcorrenteMesmoCarrinho: duas criações simultâneas do mesmo
// carrinho → exatamente uma vence (índice único parcial).
func TestCriar_ConcorrenteMesmoCarrinho(t *testing.T) {
	a := novoAmbiente(t)
	for rodada := 1; rodada <= 5; rodada++ {
		sessaoID := novaSessao(t)
		car := a.carrinho(t, sessaoID, "A1")
		largada := make(chan struct{})
		var wg sync.WaitGroup
		rs := make([]resposta, 2)
		for i := range rs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-largada
				rs[i] = a.criar(car, sessaoID, "A1")
			}(i)
		}
		close(largada)
		wg.Wait()
		criados, pendentes := 0, 0
		for _, r := range rs {
			switch {
			case r.code == http.StatusCreated:
				criados++
			case r.code == http.StatusConflict && strings.Contains(r.corpo, "pedido_pendente"):
				pendentes++
			default:
				t.Fatalf("rodada %d: %d %s", rodada, r.code, r.corpo)
			}
		}
		if criados != 1 || pendentes != 1 {
			t.Fatalf("rodada %d: criados=%d pendentes=%d", rodada, criados, pendentes)
		}
	}
}

// TestTransicionar_CAS cobre CA10: N caminhos concorrentes tentam o pivô do
// mesmo pedido → exatamente 1 vence; a trilha registra 1 transição.
func TestTransicionar_CAS(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	p := criadoDe(t, a.criar(a.carrinho(t, sessaoID, "C9"), sessaoID, "C9")).Pedido
	const n = 20
	largada := make(chan struct{})
	var wg sync.WaitGroup
	erros := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-largada
			erros[i] = outbox.WithTx(context.Background(), pool, func(tx outbox.Tx) error {
				_, err := repositorio{q: db.New(tx)}.transicionar(context.Background(), p.ID, AguardandoPagamento, PagamentoConfirmado, a.rel.agora())
				return err
			})
		}(i)
	}
	close(largada)
	wg.Wait()
	venceu := 0
	for i, err := range erros {
		switch {
		case err == nil:
			venceu++
		case !errors.Is(err, ErrTransicaoConcorrente):
			t.Fatalf("caminho %d: %v", i, err)
		}
	}
	if venceu != 1 || statusDe(t, p.ID) != string(Pago) {
		t.Fatalf("venceram %d; status %s", venceu, statusDe(t, p.ID))
	}
	if got := trilha(t, p.ID); got != "->aguardando_pagamento aguardando_pagamento>pago" {
		t.Fatalf("trilha: %s", got)
	}
}

// TestTetoContaHoldPreso (pendência da auditoria 0022): holds presos a um
// pedido contam no teto de 6 holds do carrinho.
func TestTetoContaHoldPreso(t *testing.T) {
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	seis := []string{"A1", "A2", "A3", "A4", "A5", "A6"}
	car := a.carrinho(t, sessaoID, seis...)
	criadoDe(t, a.criar(car, sessaoID, seis...))
	d, _ := reserva.DonoDoToken(car)
	if _, err := a.reserva.Travar(context.Background(), sessaoID, []string{"B1"}, d); !errors.Is(err, reserva.ErrLimiteHolds) {
		t.Fatalf("7º hold com 6 presos: %v", err)
	}
}

// TestCriar_RateLimit cobre CA11 (5/min por carrinho).
func TestCriar_RateLimit(t *testing.T) {
	a := montarAmbiente(t, limites{ip: 100, dono: 2})
	sessaoID := novaSessao(t)
	car := a.carrinho(t, sessaoID)
	for i := range 2 {
		if r := a.criar(car, sessaoID, "A1"); r.code != http.StatusConflict {
			t.Fatalf("tentativa %d: %d %s", i+1, r.code, r.corpo)
		}
	}
	if r := a.criar(car, sessaoID, "A1"); r.code != http.StatusTooManyRequests {
		t.Fatalf("3ª: %d %s", r.code, r.corpo)
	}
}
