//go:build integration
// +build integration

package identidade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/mclovin137/morfeu/internal/identidade/db"
)

// Suíte da T2 (PRD 0010): refresh rotativo, reuso, corrida, logout,
// pseudonimização e limpeza — mesmo TestMain (PG + Redis reais).

var ipSeq atomic.Int64

type respostaCookie struct {
	status    int
	corpo     string
	setCookie string
	refresh   string
}

// reqSessao chama uma rota de sessão com cookie de refresh e (opcionalmente)
// o header anti-CSRF.
func (a *ambiente) reqSessao(t *testing.T, metodo, caminho, refresh, bearer string, antiCSRF bool) respostaCookie {
	t.Helper()
	r := httptest.NewRequest(metodo, caminho, bytes.NewReader(nil))
	r.RemoteAddr = "203.0.113.1:40000"
	if refresh != "" {
		r.AddCookie(&http.Cookie{Name: cookieRefresh, Value: refresh})
	}
	if antiCSRF {
		r.Header.Set(headerAntiCSRF, valorAntiCSRF)
	}
	if bearer != "" {
		r.Header.Set(echo.HeaderAuthorization, "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, r)
	return extrairCookie(rec)
}

func extrairCookie(rec *httptest.ResponseRecorder) respostaCookie {
	out := respostaCookie{status: rec.Code, corpo: strings.TrimSpace(rec.Body.String()), setCookie: rec.Header().Get("Set-Cookie")}
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieRefresh {
			out.refresh = c.Value
		}
	}
	return out
}

// loginComSessao registra e faz login; devolve access e refresh.
func (a *ambiente) loginComSessao(t *testing.T, email string) (string, respostaCookie) {
	t.Helper()
	// IP próprio por cadastro: o limite de 10 cadastros/h por IP (0009) é
	// real e barraria os 20 usuários do teste de corrida.
	ipCadastro := fmt.Sprintf("10.%d.%d.%d", ipSeq.Add(1)%250, ipSeq.Load()/250%250, 1)
	if r := a.req(t, http.MethodPost, "/auth/registro", ipCadastro, "",
		map[string]string{"nome": "Sessão Teste", "email": email, "senha": "senha-forte-1"}); r.status != http.StatusCreated {
		t.Fatalf("cadastro: %d %s", r.status, r.corpo)
	}
	b, _ := json.Marshal(map[string]string{"email": email, "senha": "senha-forte-1"})
	r := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(b))
	r.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	r.RemoteAddr = "203.0.113.2:40000"
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, r)
	res := extrairCookie(rec)
	if res.status != http.StatusOK || res.refresh == "" {
		t.Fatalf("login sem sessão: %d %s", res.status, res.corpo)
	}
	var s struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal([]byte(res.corpo), &s)
	return s.AccessToken, res
}

func linhaRefresh(t *testing.T, token string) (familia uuid.UUID, usado, revogado bool, ok bool) {
	t.Helper()
	soma := sha256.Sum256([]byte(token))
	var u, r *time.Time
	err := pool.QueryRow(context.Background(),
		"SELECT familia_id, usado_em, revogado_em FROM refresh_token WHERE hash = $1", soma[:]).Scan(&familia, &u, &r)
	if err != nil {
		return uuid.Nil, false, false, false
	}
	return familia, u != nil, r != nil, true
}

func familiaRevogada(t *testing.T, familia uuid.UUID) bool {
	t.Helper()
	var abertas int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM refresh_token WHERE familia_id = $1 AND revogado_em IS NULL", familia).Scan(&abertas); err != nil {
		t.Fatalf("contar família: %v", err)
	}
	return abertas == 0
}

// TestRefresh_CookieEHashNoBanco cobre CA01.
func TestRefresh_CookieEHashNoBanco(t *testing.T) {
	a := novoAmbiente(t, 4)
	_, login := a.loginComSessao(t, emailUnico())

	for _, atributo := range []string{"HttpOnly", "Secure", "SameSite=Strict", "Path=/auth/refresh", "Max-Age=604800"} {
		if !strings.Contains(login.setCookie, atributo) {
			t.Errorf("cookie sem %s: %s", atributo, login.setCookie)
		}
	}
	if _, _, _, ok := linhaRefresh(t, login.refresh); !ok {
		t.Fatal("hash SHA-256 do cookie deveria estar no banco")
	}
	var emClaro int
	_ = pool.QueryRow(context.Background(), "SELECT count(*) FROM refresh_token WHERE hash = $1", []byte(login.refresh)).Scan(&emClaro)
	if emClaro != 0 {
		t.Error("token em claro não pode estar no banco")
	}
}

// TestRefresh_RotacaoCSRFEReuso cobre CA02, CA03 e CA04.
func TestRefresh_RotacaoCSRFEReuso(t *testing.T) {
	a := novoAmbiente(t, 4)
	_, login := a.loginComSessao(t, emailUnico())
	familia, _, _, _ := linhaRefresh(t, login.refresh)

	if r := a.reqSessao(t, http.MethodPost, "/auth/refresh", login.refresh, "", false); r.status != http.StatusForbidden {
		t.Fatalf("sem header anti-CSRF deveria ser 403: %d", r.status)
	}
	if _, usado, _, _ := linhaRefresh(t, login.refresh); usado {
		t.Fatal("403 de CSRF não pode consumir o token")
	}

	novo := a.reqSessao(t, http.MethodPost, "/auth/refresh", login.refresh, "", true)
	if novo.status != http.StatusOK || novo.refresh == "" || novo.refresh == login.refresh {
		t.Fatalf("rotação: %d %s", novo.status, novo.corpo)
	}
	var s struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal([]byte(novo.corpo), &s)
	if _, err := a.emissor.Validar(s.AccessToken); err != nil {
		t.Errorf("access novo inválido: %v", err)
	}
	if fam, _, _, _ := linhaRefresh(t, novo.refresh); fam != familia {
		t.Error("sucessor deveria estar na mesma família")
	}
	if _, usado, _, _ := linhaRefresh(t, login.refresh); !usado {
		t.Error("token antigo deveria estar marcado como usado")
	}

	// Reuso do antigo: família inteira morre, inclusive o sucessor.
	reuso := a.reqSessao(t, http.MethodPost, "/auth/refresh", login.refresh, "", true)
	if reuso.status != http.StatusUnauthorized || !strings.Contains(reuso.setCookie, "Max-Age=0") {
		t.Fatalf("reuso deveria dar 401 e limpar o cookie: %d %s", reuso.status, reuso.setCookie)
	}
	if !familiaRevogada(t, familia) {
		t.Error("reuso deveria revogar a família inteira")
	}
	if r := a.reqSessao(t, http.MethodPost, "/auth/refresh", novo.refresh, "", true); r.status != http.StatusUnauthorized {
		t.Errorf("sucessor da família revogada não pode renovar: %d", r.status)
	}
	// 2 reusos: o antigo (usado) e depois o sucessor (já revogado) — token
	// revogado reapresentado também é reuso (RF04).
	if n := a.logs.FilterMessage("refresh_reuso_detectado").Len(); n != 2 {
		t.Errorf("esperava 2 logs de reuso, veio %d", n)
	}
}

// TestRefresh_CorridaMesmoToken cobre CA05: 20 iterações, 2 refreshes
// simultâneos com o mesmo token → exatamente 1 sucesso e família revogada.
func TestRefresh_CorridaMesmoToken(t *testing.T) {
	a := novoAmbiente(t, 4)
	for i := 0; i < 20; i++ {
		_, login := a.loginComSessao(t, emailUnico())
		familia, _, _, _ := linhaRefresh(t, login.refresh)

		largada := make(chan struct{})
		var wg sync.WaitGroup
		status := make([]int, 2)
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				<-largada
				status[g] = a.reqSessao(t, http.MethodPost, "/auth/refresh", login.refresh, "", true).status
			}(g)
		}
		close(largada)
		wg.Wait()

		sucessos := 0
		for _, st := range status {
			if st == http.StatusOK {
				sucessos++
			} else if st != http.StatusUnauthorized {
				t.Fatalf("iteração %d: status inesperado %v", i, status)
			}
		}
		if sucessos != 1 {
			t.Fatalf("iteração %d: esperava exatamente 1 sucesso, veio %v", i, status)
		}
		if !familiaRevogada(t, familia) {
			t.Fatalf("iteração %d: família deveria terminar revogada", i)
		}
	}
}

// TestLogout cobre CA06.
func TestLogout(t *testing.T) {
	a := novoAmbiente(t, 4)
	_, login := a.loginComSessao(t, emailUnico())
	familia, _, _, _ := linhaRefresh(t, login.refresh)

	r := a.reqSessao(t, http.MethodPost, "/auth/refresh/logout", login.refresh, "", true)
	if r.status != http.StatusNoContent || !strings.Contains(r.setCookie, "Max-Age=0") {
		t.Fatalf("logout: %d %s", r.status, r.setCookie)
	}
	if !familiaRevogada(t, familia) {
		t.Error("logout deveria revogar a família no servidor")
	}
	if r := a.reqSessao(t, http.MethodPost, "/auth/refresh", login.refresh, "", true); r.status != http.StatusUnauthorized {
		t.Errorf("refresh após logout deveria ser 401: %d", r.status)
	}
	if r := a.reqSessao(t, http.MethodPost, "/auth/refresh/logout", "", "", true); r.status != http.StatusNoContent {
		t.Errorf("logout sem cookie deveria ser idempotente (204): %d", r.status)
	}
}

// TestRemoverConta cobre CA07.
func TestRemoverConta(t *testing.T) {
	a := novoAmbiente(t, 4)
	email := emailUnico()
	access, login := a.loginComSessao(t, email)
	claims, _ := a.emissor.Validar(access)
	id := claims.UsuarioIDDe()

	r := a.reqSessao(t, http.MethodDelete, "/auth/conta", "", access, false)
	if r.status != http.StatusNoContent {
		t.Fatalf("remover conta: %d %s", r.status, r.corpo)
	}

	var nome, emailGravado, hash, papel string
	if err := pool.QueryRow(context.Background(), "SELECT nome, email, senha_hash, papel FROM usuario WHERE id = $1", id).
		Scan(&nome, &emailGravado, &hash, &papel); err != nil {
		t.Fatalf("usuário deveria continuar existindo (id preservado): %v", err)
	}
	if nome != "Conta removida" || emailGravado != "removido+"+id.String()+"@invalido.local" || hash != "!" || papel != "cliente" {
		t.Errorf("pseudonimização fora do oráculo: %q %q %q %q", nome, emailGravado, hash, papel)
	}
	if r := a.login(t, "203.0.113.3", email, "senha-forte-1"); r.status != http.StatusUnauthorized {
		t.Errorf("login com credenciais antigas deveria falhar: %d", r.status)
	}
	if r := a.reqSessao(t, http.MethodPost, "/auth/refresh", login.refresh, "", true); r.status != http.StatusUnauthorized {
		t.Errorf("refresh da conta removida deveria falhar: %d", r.status)
	}
	if r := a.registrar(t, email, "senha-nova-123"); r.status != http.StatusCreated {
		t.Errorf("e-mail original deveria ficar livre para novo cadastro: %d %s", r.status, r.corpo)
	}

	// Operador não se autoexclui pela API.
	emailOp := emailUnico()
	senhaOp, _, _ := SeedOperador(context.Background(), db.New(pool), parametrosRapidos, a.servico.logger, "Op", emailOp)
	lr := a.login(t, "203.0.113.4", emailOp, senhaOp)
	var s struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal([]byte(lr.corpo), &s)
	if r := a.reqSessao(t, http.MethodDelete, "/auth/conta", "", s.AccessToken, false); r.status != http.StatusForbidden {
		t.Errorf("operador deveria receber 403: %d", r.status)
	}
}

// TestLimparRefreshExpirados cobre CA08.
func TestLimparRefreshExpirados(t *testing.T) {
	a := novoAmbiente(t, 4)
	_, login := a.loginComSessao(t, emailUnico())
	familia, _, _, _ := linhaRefresh(t, login.refresh)
	var usuarioID uuid.UUID
	_ = pool.QueryRow(context.Background(), "SELECT usuario_id FROM refresh_token WHERE familia_id = $1", familia).Scan(&usuarioID)

	q := db.New(pool)
	vencido := uuid.New()
	if err := q.InserirRefresh(context.Background(), db.InserirRefreshParams{
		ID: vencido, UsuarioID: usuarioID, FamiliaID: familia, Hash: []byte("vencido-" + vencido.String()), ExpiraEm: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("inserir vencido: %v", err)
	}
	n, err := LimparRefreshExpirados(context.Background(), q)
	if err != nil || n < 1 {
		t.Fatalf("limpeza: n=%d err=%v", n, err)
	}
	var existe int
	_ = pool.QueryRow(context.Background(), "SELECT count(*) FROM refresh_token WHERE id = $1", vencido).Scan(&existe)
	if existe != 0 {
		t.Error("refresh vencido deveria ter sido apagado")
	}
	if _, _, _, ok := linhaRefresh(t, login.refresh); !ok {
		t.Error("refresh válido não pode ser apagado")
	}
}
