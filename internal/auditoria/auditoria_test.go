//go:build integration
// +build integration

package auditoria

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mclovin137/morfeu/internal/outbox"
)

// Trilha de auditoria em PG real com a migration 017 (PRD 0037).
var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "auditoria"},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "postgres:", err)
		os.Exit(1)
	}
	code := func() int {
		host, _ := pg.Host(ctx)
		porta, _ := pg.MappedPort(ctx, "5432/tcp")
		pool, err = pgxpool.New(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%s/auditoria?sslmode=disable", host, porta.Port()))
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		defer pool.Close()
		ddl, err := os.ReadFile("../../migrations/017_eventos_auditoria.up.sql")
		if err == nil {
			_, err = pool.Exec(ctx, string(ddl))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "migration 017:", err)
			return 1
		}
		return m.Run()
	}()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

func registrar(t *testing.T, ctx context.Context, acao Acao, alvo string, quando time.Time) error {
	t.Helper()
	return outbox.WithTx(ctx, pool, func(tx outbox.Tx) error { return Registrar(ctx, tx, acao, alvo, quando) })
}

func contar(t *testing.T, alvo string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM eventos_auditoria WHERE alvo_id = $1`, alvo).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestRegistrar: o ator vem do context (sem ator = sistema); só IDs.
func TestRegistrar(t *testing.T) {
	op := uuid.New()
	alvo := uuid.NewString()
	agora := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := registrar(t, ComAtor(context.Background(), op), PedidoCancelado, alvo, agora); err != nil {
		t.Fatal(err)
	}
	var ator uuid.UUID
	var acao, tipo string
	if err := pool.QueryRow(context.Background(), `SELECT ator_id, acao, alvo_tipo FROM eventos_auditoria WHERE alvo_id = $1`, alvo).
		Scan(&ator, &acao, &tipo); err != nil || ator != op || acao != string(PedidoCancelado) || tipo != "pedido" {
		t.Fatalf("evento: %v %s %s %v", ator, acao, tipo, err)
	}
	if err := registrar(t, context.Background(), FilmeCriado, "4242", agora); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT ator_id FROM eventos_auditoria WHERE alvo_id = '4242'`).Scan(&ator); err != nil || ator != uuid.Nil {
		t.Fatalf("sem ator deveria ser o sistema: %v %v", ator, err)
	}
	if err := registrar(t, context.Background(), Acao("apagar_tudo"), "1", agora); err == nil {
		t.Fatal("ação fora do enum deveria falhar")
	}
}

// TestRegistrar_SemPII: o CHECK do alvo recusa texto livre (e-mail, código).
func TestRegistrar_SemPII(t *testing.T) {
	for _, alvo := range []string{"ana@exemplo.com", "ABCDEFGHIJKLMNOP", "1; DROP TABLE x", ""} {
		if err := registrar(t, context.Background(), PedidoCancelado, alvo, time.Now()); err == nil {
			t.Errorf("alvo %q deveria ser recusado", alvo)
		}
	}
}

// TestAppendOnly: UPDATE e TRUNCATE barrados pelo trigger.
func TestAppendOnly(t *testing.T) {
	ctx := context.Background()
	if err := registrar(t, ctx, SalaCriada, "77", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE eventos_auditoria SET ator_id = $1 WHERE alvo_id = '77'`, uuid.New()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE deveria falhar: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE eventos_auditoria`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("TRUNCATE deveria falhar: %v", err)
	}
	if contar(t, "77") != 1 {
		t.Fatal("evento sumiu")
	}
}

// TestPurgar: remove só o que passou da retenção, em lotes, idempotente.
func TestPurgar(t *testing.T) {
	ctx := context.Background()
	agora := time.Date(2099, 6, 1, 0, 0, 0, 0, time.UTC)
	limite := agora.Add(-Retencao)
	for i := range 7 {
		if err := registrar(t, ctx, SessaoCriada, "9000"+fmt.Sprint(i), limite.Add(-time.Duration(i+1)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := registrar(t, ctx, SessaoCriada, "90010", limite.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err := Purgar(ctx, pool, limite, 3)
	if err != nil || n < 7 {
		t.Fatalf("purga: n=%d err=%v", n, err)
	}
	for i := range 7 {
		if contar(t, "9000"+fmt.Sprint(i)) != 0 {
			t.Fatalf("evento %d além da retenção ficou", i)
		}
	}
	if contar(t, "90010") != 1 {
		t.Fatal("evento dentro da retenção não pode sair")
	}
	if n, err := Purgar(ctx, pool, limite, 3); err != nil || n != 0 {
		t.Fatalf("2ª purga: n=%d err=%v", n, err)
	}
}
