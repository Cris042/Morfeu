package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/catalogo"
	catalogodb "github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/config"
	"github.com/mclovin137/morfeu/internal/logger"
)

// TestRotasBackoffice_ExigemOperador (PRD 0037): percorre TODAS as rotas
// /backoffice/* que o main registra — rota nova esquecida fora do RBAC
// falha aqui. Sem token → 401; cliente → 403. O pool e o Redis apontam para
// endereços que nunca são contatados (o RBAC recusa antes de qualquer I/O).
func TestRotasBackoffice_ExigemOperador(t *testing.T) {
	t.Setenv("JWT_SEGREDO", "segredo-jwt-de-teste-com-32-bytes!!")
	t.Setenv("JWT_KID", "k1")
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), "postgres://x:x@127.0.0.1:1/x?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer func() { _ = rdb.Close() }()
	log := logger.NewLogger("error")
	cs := catalogo.NovoServico(catalogodb.New(pool), pool, nil, log.Logger)
	e := echo.New()
	registrarRotasDeDominio(e, modeAPI, cfg, pool, rdb, nil, cs, catalogo.NovoHandler(cs, log.Logger), log)

	emissor, err := autenticacao.NovoEmissor(autenticacao.ConfigJWT{Segredo: []byte(cfg.JWTSegredo), Kid: cfg.JWTKid, TTL: autenticacao.TTLAccessPadrao})
	if err != nil {
		t.Fatal(err)
	}
	cliente, _, _ := emissor.Emitir(uuid.New(), autenticacao.PapelCliente)
	parametro := regexp.MustCompile(`:[a-z_]+`)
	vistas := 0
	for _, r := range e.Routes() {
		if !strings.HasPrefix(r.Path, "/backoffice") {
			continue
		}
		vistas++
		caminho := parametro.ReplaceAllString(r.Path, "1")
		for _, c := range []struct {
			token string
			quer  int
		}{{"", http.StatusUnauthorized}, {cliente, http.StatusForbidden}} {
			req := httptest.NewRequest(r.Method, caminho, strings.NewReader("{}"))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			if c.token != "" {
				req.Header.Set(echo.HeaderAuthorization, "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != c.quer {
				t.Errorf("%s %s (token=%t): %d, quer %d", r.Method, r.Path, c.token != "", rec.Code, c.quer)
			}
		}
	}
	// filmes (6) + salas (3) + sessões (3) + pedidos (3).
	if vistas < 15 {
		t.Fatalf("só %d rotas /backoffice registradas — o teste perdeu o alcance", vistas)
	}
}
