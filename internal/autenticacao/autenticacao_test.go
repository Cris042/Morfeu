package autenticacao

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	dto "github.com/prometheus/client_model/go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/telemetria"
)

var segredoTeste = []byte("segredo-de-teste-com-32-bytes-ok!")

// relogio é um relógio manual (sem sleep nos testes — RNF04).
type relogio struct{ t time.Time }

func (r *relogio) agora() time.Time        { return r.t }
func (r *relogio) avancar(d time.Duration) { r.t = r.t.Add(d) }

func novoRelogio() *relogio { return &relogio{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)} }

func emissorTeste(t *testing.T, r *relogio) *Emissor {
	t.Helper()
	e, err := NovoEmissor(ConfigJWT{Segredo: segredoTeste, Kid: "k1", TTL: TTLAccessPadrao, Agora: r.agora})
	if err != nil {
		t.Fatalf("NovoEmissor: %v", err)
	}
	return e
}

func parteJSON(t *testing.T, parte string) map[string]any {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(parte)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json: %v", err)
	}
	return m
}

func chaves(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// TestEmitir_HeaderEClaimsSemPII cobre CA01.
func TestEmitir_HeaderEClaimsSemPII(t *testing.T) {
	r := novoRelogio()
	token, expira, err := emissorTeste(t, r).Emitir(uuid.New(), PapelCliente)
	if err != nil {
		t.Fatalf("Emitir: %v", err)
	}
	partes := strings.Split(token, ".")
	if len(partes) != 3 {
		t.Fatalf("token deveria ter 3 partes: %q", token)
	}
	h := parteJSON(t, partes[0])
	if h["alg"] != "HS256" || h["kid"] != "k1" {
		t.Errorf("header inesperado: %v", h)
	}
	p := parteJSON(t, partes[1])
	if got := strings.Join(chaves(p), ","); got != "exp,iat,papel,sub" {
		t.Errorf("claims devem ser exatamente exp,iat,papel,sub; vieram %s", got)
	}
	if int64(p["exp"].(float64))-int64(p["iat"].(float64)) != int64(TTLAccessPadrao.Seconds()) {
		t.Errorf("exp - iat deveria ser %s: %v", TTLAccessPadrao, p)
	}
	if !expira.Equal(r.t.Add(TTLAccessPadrao)) {
		t.Errorf("expiraEm %v != %v", expira, r.t.Add(TTLAccessPadrao))
	}
}

func assinar(t *testing.T, metodo jwt.SigningMethod, chave any, claims jwt.Claims, kid any) string {
	t.Helper()
	tk := jwt.NewWithClaims(metodo, claims)
	if kid != nil {
		tk.Header["kid"] = kid
	}
	s, err := tk.SignedString(chave)
	if err != nil {
		t.Fatalf("assinar: %v", err)
	}
	return s
}

// TestValidar_TokensHostis cobre CA02.
func TestValidar_TokensHostis(t *testing.T) {
	r := novoRelogio()
	e := emissorTeste(t, r)
	valido, _, _ := e.Emitir(uuid.New(), PapelOperador)

	agora := jwt.NewNumericDate(r.t)
	exp := jwt.NewNumericDate(r.t.Add(time.Minute))
	claims := func(sub string, papel Papel, comExp bool) Claims {
		c := Claims{Papel: papel, RegisteredClaims: jwt.RegisteredClaims{Subject: sub, IssuedAt: agora}}
		if comExp {
			c.ExpiresAt = exp
		}
		return c
	}
	id := uuid.NewString()

	partes := strings.Split(valido, ".")
	payloadAdulterado := parteJSON(t, partes[1])
	payloadAdulterado["papel"] = "operador"
	payloadAdulterado["sub"] = uuid.NewString()
	b, _ := json.Marshal(payloadAdulterado)
	adulterado := partes[0] + "." + base64.RawURLEncoding.EncodeToString(b) + "." + partes[2]

	semAssinatura := assinar(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims(id, PapelOperador, true), "k1")

	casos := map[string]string{
		"alg none":           semAssinatura,
		"HS384":              assinar(t, jwt.SigningMethodHS384, segredoTeste, claims(id, PapelCliente, true), "k1"),
		"HS512":              assinar(t, jwt.SigningMethodHS512, segredoTeste, claims(id, PapelCliente, true), "k1"),
		"outro segredo":      assinar(t, jwt.SigningMethodHS256, []byte("outro-segredo-com-32-bytes-tambem!"), claims(id, PapelCliente, true), "k1"),
		"payload adulterado": adulterado,
		"sem exp":            assinar(t, jwt.SigningMethodHS256, segredoTeste, claims(id, PapelCliente, false), "k1"),
		"kid diferente":      assinar(t, jwt.SigningMethodHS256, segredoTeste, claims(id, PapelCliente, true), "k2"),
		"sem kid":            assinar(t, jwt.SigningMethodHS256, segredoTeste, claims(id, PapelCliente, true), nil),
		"sub não-UUID":       assinar(t, jwt.SigningMethodHS256, segredoTeste, claims("joao@exemplo.com", PapelCliente, true), "k1"),
		"papel inválido":     assinar(t, jwt.SigningMethodHS256, segredoTeste, claims(id, Papel("admin"), true), "k1"),
		"lixo":               "nao.e.jwt",
		"vazio":              "",
	}
	for nome, tk := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := e.Validar(tk); err == nil {
				t.Fatal("token hostil foi aceito")
			}
		})
	}

	t.Run("expirado", func(t *testing.T) {
		r2 := novoRelogio()
		e2 := emissorTeste(t, r2)
		tk, _, _ := e2.Emitir(uuid.New(), PapelCliente)
		r2.avancar(TTLAccessPadrao + time.Second)
		if _, err := e2.Validar(tk); err == nil {
			t.Fatal("token expirado foi aceito")
		}
	})

	t.Run("válido", func(t *testing.T) {
		c, err := e.Validar(valido)
		if err != nil || c.Papel != PapelOperador {
			t.Fatalf("token válido rejeitado: %v", err)
		}
	})
}

// TestNovoEmissor_ConfigInvalida cobre CA06.
func TestNovoEmissor_ConfigInvalida(t *testing.T) {
	casos := map[string]ConfigJWT{
		"segredo 31 bytes": {Segredo: make([]byte, 31), Kid: "k1", TTL: time.Minute},
		"kid vazio":        {Segredo: segredoTeste, TTL: time.Minute},
		"ttl zero":         {Segredo: segredoTeste, Kid: "k1"},
	}
	for nome, cfg := range casos {
		if _, err := NovoEmissor(cfg); err == nil {
			t.Errorf("%s: deveria falhar", nome)
		}
	}
}

// TestExigir_Matriz cobre CA03: status exato por token × papéis exigidos;
// contexto só é populado quando passa.
func TestExigir_Matriz(t *testing.T) {
	r := novoRelogio()
	e := emissorTeste(t, r)
	idCliente, idOperador := uuid.New(), uuid.New()
	cliente, _, _ := e.Emitir(idCliente, PapelCliente)
	operador, _, _ := e.Emitir(idOperador, PapelOperador)

	// emitido 1h antes do relógio de e: já expirou para e
	expirado, _, _ := emissorTeste(t, &relogio{t: r.t.Add(-time.Hour)}).Emitir(uuid.New(), PapelOperador)
	adulterado := operador[:len(operador)-2] + "xx"

	headers := map[string]string{
		"sem header":        "",
		"basic":             "Basic dXNlcjpzZW5oYQ==",
		"bearer malformado": "Bearer",
		"expirado":          "Bearer " + expirado,
		"adulterado":        "Bearer " + adulterado,
		"cliente":           "Bearer " + cliente,
		"operador":          "bearer " + operador,
	}
	exigencias := map[string][]Papel{
		"cliente":  {PapelCliente},
		"operador": {PapelOperador},
		"ambos":    {PapelCliente, PapelOperador},
		"nenhum":   {},
	}
	esperado := map[string]map[string]int{
		"sem header":        {"cliente": 401, "operador": 401, "ambos": 401, "nenhum": 401},
		"basic":             {"cliente": 401, "operador": 401, "ambos": 401, "nenhum": 401},
		"bearer malformado": {"cliente": 401, "operador": 401, "ambos": 401, "nenhum": 401},
		"expirado":          {"cliente": 401, "operador": 401, "ambos": 401, "nenhum": 401},
		"adulterado":        {"cliente": 401, "operador": 401, "ambos": 401, "nenhum": 401},
		"cliente":           {"cliente": 200, "operador": 403, "ambos": 200, "nenhum": 403},
		"operador":          {"cliente": 403, "operador": 200, "ambos": 200, "nenhum": 403},
	}

	for nomeH, header := range headers {
		for nomeE, papeis := range exigencias {
			t.Run(nomeH+"×"+nomeE, func(t *testing.T) {
				verificarExigir(t, e, papeis, header, esperado[nomeH][nomeE], nomeH,
					map[string]uuid.UUID{"cliente": idCliente, "operador": idOperador}[nomeH])
			})
		}
	}
}

// verificarExigir executa uma célula da matriz: status exato, contexto
// populado só no 200 e corpos genéricos nos erros.
func verificarExigir(t *testing.T, e *Emissor, papeis []Papel, header string, want int, papelEsperado string, idEsperado uuid.UUID) {
	t.Helper()
	ec := echo.New()
	var gotID uuid.UUID
	var gotPapel Papel
	ec.GET("/p", func(c echo.Context) error {
		gotID, _ = UsuarioID(c)
		gotPapel, _ = PapelDe(c)
		return c.NoContent(http.StatusOK)
	}, Exigir(e, papeis...))

	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	if header != "" {
		req.Header.Set(echo.HeaderAuthorization, header)
	}
	rec := httptest.NewRecorder()
	ec.ServeHTTP(rec, req)

	if rec.Code != want {
		t.Fatalf("status %d, esperado %d (corpo %s)", rec.Code, want, rec.Body)
	}
	switch rec.Code {
	case http.StatusOK:
		if gotID != idEsperado || string(gotPapel) != papelEsperado {
			t.Errorf("contexto errado: id=%v papel=%v", gotID, gotPapel)
		}
	case http.StatusUnauthorized:
		if rec.Header().Get(echo.HeaderWWWAuthenticate) != "Bearer" || !strings.Contains(rec.Body.String(), "nao_autenticado") {
			t.Errorf("401 sem WWW-Authenticate/corpo genérico: %v %s", rec.Header(), rec.Body)
		}
	case http.StatusForbidden:
		if !strings.Contains(rec.Body.String(), "acesso_negado") {
			t.Errorf("403 sem corpo genérico: %s", rec.Body)
		}
	}
}

// redisInalcancavel simula o Redis fora do ar (porta sem servidor).
func redisInalcancavel() *redis.Client {
	return redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
}

// TestLimitador_FallbackMemoria cobre RF06 com o Redis fora: bloqueia na
// Max-ésima falha, libera após a janela (relógio manual), Limpar zera.
func TestLimitador_FallbackMemoria(t *testing.T) {
	r := novoRelogio()
	cli := redisInalcancavel()
	defer func() { _ = cli.Close() }()
	l, err := NovoLimitador(ConfigLimitador{Redis: cli, Prefixo: "t:", Max: 3, Janela: 5 * time.Minute, Agora: r.agora}, zap.NewNop())
	if err != nil {
		t.Fatalf("NovoLimitador: %v", err)
	}
	ctx := context.Background()
	chave := "conta:joao@exemplo.com"

	for i := 0; i < 3; i++ {
		if l.Bloqueado(ctx, chave) {
			t.Fatalf("bloqueado cedo demais na tentativa %d", i+1)
		}
		l.RegistrarFalha(ctx, chave)
	}
	if !l.Bloqueado(ctx, chave) {
		t.Fatal("deveria bloquear após 3 falhas (fallback em memória)")
	}
	if l.Bloqueado(ctx, "conta:outra") {
		t.Fatal("outra chave não pode ser afetada")
	}
	r.avancar(5 * time.Minute)
	if l.Bloqueado(ctx, chave) {
		t.Fatal("deveria liberar após a janela")
	}
	l.RegistrarFalha(ctx, chave)
	l.Limpar(ctx, chave)
	if l.Bloqueado(ctx, chave) {
		t.Fatal("Limpar deveria zerar a chave")
	}
}

// TestLimitador_MemoriaCheiaBloqueiaNoFallback: com o Redis fora e o teto de
// entradas atingido, chave nova conta como bloqueada (nunca sem limite).
func TestLimitador_MemoriaCheiaBloqueiaNoFallback(t *testing.T) {
	r := novoRelogio()
	cli := redisInalcancavel()
	defer func() { _ = cli.Close() }()
	l, _ := NovoLimitador(ConfigLimitador{Redis: cli, Prefixo: "t:", Max: 5, Janela: time.Minute, Agora: r.agora}, zap.NewNop())
	for i := 0; i < maxEntradasMemoria; i++ {
		l.memoria[l.chaveRedis("ip:"+uuid.NewString())] = entradaMemoria{falhas: 1, expira: r.t.Add(time.Minute)}
	}
	if !l.Bloqueado(context.Background(), "ip:novo") {
		t.Fatal("mapa cheio no fallback deveria bloquear chave nova")
	}
}

// TestMetricas_LabelsDaAllowlist cobre CA07: instrumentos de auth aparecem no
// registry da telemetria real só com labels permitidos.
func TestMetricas_LabelsDaAllowlist(t *testing.T) {
	tel, err := telemetria.Iniciar(context.Background(), telemetria.Config{Servico: "t", Versao: "t", TaxaAmostragem: 1})
	if err != nil {
		t.Fatalf("telemetria: %v", err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()

	m, err := NovasMetricas()
	if err != nil {
		t.Fatalf("NovasMetricas: %v", err)
	}
	ctx := context.Background()
	m.Login(ctx, ResultadoSucesso, 30*time.Millisecond)
	m.Login(ctx, ResultadoCredenciaisInvalidas, 25*time.Millisecond)
	m.Bloqueio(ctx, EscopoConta)
	m.ReusoRefresh(ctx)

	familias, err := tel.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	vistos, fora := nomesELabelsFora(familias, "resultado", "escopo")
	if len(fora) > 0 {
		t.Errorf("labels fora do esperado: %v", fora)
	}
	for _, nome := range []string{"auth_login_total", "auth_login_duracao_segundos", "auth_ratelimit_bloqueios_total", "auth_refresh_reuso_total"} {
		if !vistos[nome] {
			t.Errorf("métrica %s ausente; vistas: %v", nome, vistos)
		}
	}
}

// nomesELabelsFora devolve os nomes das famílias e os labels não permitidos.
func nomesELabelsFora(familias []*dto.MetricFamily, permitidos ...string) (map[string]bool, []string) {
	ok := map[string]bool{}
	for _, p := range permitidos {
		ok[p] = true
	}
	vistos := map[string]bool{}
	var fora []string
	for _, f := range familias {
		vistos[f.GetName()] = true
		for _, mt := range f.GetMetric() {
			for _, lp := range mt.GetLabel() {
				if !ok[lp.GetName()] {
					fora = append(fora, f.GetName()+"{"+lp.GetName()+"}")
				}
			}
		}
	}
	return vistos, fora
}

// TestOpcional cobre o PRD 0031: sem header segue anônimo; header presente e
// inválido → 401 (nunca cai para anônimo); válido → usuário no contexto.
func TestOpcional(t *testing.T) {
	r := novoRelogio()
	e := emissorTeste(t, r)
	id := uuid.New()
	cliente, _, _ := e.Emitir(id, PapelCliente)
	expirado, _, _ := emissorTeste(t, &relogio{t: r.t.Add(-time.Hour)}).Emitir(uuid.New(), PapelCliente)
	casos := map[string]struct {
		header  string
		status  int
		usuario *uuid.UUID
	}{
		"sem header": {"", http.StatusOK, nil},
		"válido":     {"Bearer " + cliente, http.StatusOK, &id},
		"expirado":   {"Bearer " + expirado, http.StatusUnauthorized, nil},
		"adulterado": {"Bearer " + cliente[:len(cliente)-2] + "xx", http.StatusUnauthorized, nil},
		"basic":      {"Basic dXNlcjpzZW5oYQ==", http.StatusUnauthorized, nil},
	}
	for nome, c := range casos {
		srv := echo.New()
		var visto *uuid.UUID
		srv.GET("/x", func(ctx echo.Context) error {
			if u, ok := UsuarioID(ctx); ok {
				visto = &u
			}
			return ctx.NoContent(http.StatusOK)
		}, Opcional(e))
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if c.header != "" {
			req.Header.Set(echo.HeaderAuthorization, c.header)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != c.status || (c.usuario == nil) != (visto == nil) || (c.usuario != nil && *visto != *c.usuario) {
			t.Errorf("%s: %d usuário=%v", nome, rec.Code, visto)
		}
	}
}
