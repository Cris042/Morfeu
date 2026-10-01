//go:build integration
// +build integration

package infra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mclovin137/morfeu/internal/auditoria"
)

const (
	bancoRoles     = "morfeu"
	senhaMigr      = "senha-migrator-so-de-teste"
	senhaApp       = "senha-app-so-de-teste"
	senhaPurge     = "senha-purge-so-de-teste"
	senhaBackup    = "senha-backup-so-de-teste"
	sqlstateNegado = "42501" // insufficient_privilege
)

// subirPGComRoles inicia um PG 16 com o 02-roles.sh REAL do repositório
// (ADR 0013) e devolve host:porta.
func subirPGComRoles(t *testing.T) (host, porta string) {
	t.Helper()
	ctx := context.Background()
	script, err := filepath.Abs("../../configs/postgres/02-roles.sh")
	if err != nil {
		t.Fatal(err)
	}
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres-so-de-teste", "POSTGRES_DB": bancoRoles,
				"PG_MIGRATOR_PASSWORD": senhaMigr, "PG_APP_PASSWORD": senhaApp,
				"PG_PURGE_PASSWORD": senhaPurge, "PG_BACKUP_PASSWORD": senhaBackup,
			},
			Files: []testcontainers.ContainerFile{{
				HostFilePath: script, ContainerFilePath: "/docker-entrypoint-initdb.d/02-roles.sh", FileMode: 0o755,
			}},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("subir postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })
	host, err = pg.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pg.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return host, p.Port()
}

func urlDoRole(host, porta, role, senha string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, senha, host, porta, bancoRoles)
}

func abrirPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// esperarNegado exige SQLSTATE 42501 (permission denied) — nenhum outro erro serve.
func esperarNegado(t *testing.T, err error, o string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != sqlstateNegado {
		t.Errorf("%s: esperado SQLSTATE %s, veio %v", o, sqlstateNegado, err)
	}
}

func contarInt(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// TestRolesDoPostgres cobre CA03 com os roles criados pelo 02-roles.sh de
// verdade e as migrations aplicadas por morfeu_migrator.
func TestRolesDoPostgres(t *testing.T) {
	ctx := context.Background()
	host, porta := subirPGComRoles(t)

	m, err := migrate.New("file://../../migrations", urlDoRole(host, porta, "morfeu_migrator", senhaMigr))
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil {
		t.Fatalf("migrations como morfeu_migrator: %v", err)
	}

	super := abrirPool(t, urlDoRole(host, porta, "postgres", "postgres-so-de-teste"))
	app := abrirPool(t, urlDoRole(host, porta, "morfeu_app", senhaApp))
	purge := abrirPool(t, urlDoRole(host, porta, "morfeu_purge", senhaPurge))
	backup := abrirPool(t, urlDoRole(host, porta, "morfeu_backup", senhaBackup))

	t.Run("migrator é dono de todas as tabelas; app não é dono de nenhuma", func(t *testing.T) {
		if n := contarInt(t, super, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tableowner = 'morfeu_app'`); n != 0 {
			t.Errorf("morfeu_app é dono de %d tabela(s)", n)
		}
		if n := contarInt(t, super, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tableowner <> 'morfeu_migrator'`); n != 0 {
			t.Errorf("%d tabela(s) com dono diferente do migrator", n)
		}
		if n := contarInt(t, super, `SELECT count(*) FROM pg_database WHERE datname = $1 AND pg_get_userbyid(datdba) = 'morfeu_migrator'`, bancoRoles); n != 1 {
			t.Errorf("morfeu_migrator não é dono do banco")
		}
	})

	t.Run("app faz CRUD (tabelas e sequências via default privileges)", func(t *testing.T) {
		var id int64
		if err := app.QueryRow(ctx, `INSERT INTO salas (nome, layout) VALUES ('Sala 1', '{}') RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("INSERT: %v", err)
		}
		if _, err := app.Exec(ctx, `UPDATE salas SET nome = 'Sala A' WHERE id = $1`, id); err != nil {
			t.Fatalf("UPDATE: %v", err)
		}
		if n := contarInt(t, app, `SELECT count(*) FROM salas WHERE id = $1`, id); n != 1 {
			t.Fatalf("SELECT: %d linhas", n)
		}
		if _, err := app.Exec(ctx, `DELETE FROM salas WHERE id = $1`, id); err != nil {
			t.Fatalf("DELETE: %v", err)
		}
	})

	t.Run("app não faz DDL", func(t *testing.T) {
		_, err := app.Exec(ctx, `CREATE TABLE intruso (id int)`)
		esperarNegado(t, err, "CREATE TABLE")
		_, err = app.Exec(ctx, `ALTER TABLE salas ADD COLUMN x int`)
		if err == nil {
			t.Error("ALTER TABLE pelo app deveria falhar (não é dono)")
		}
	})

	antigo := time.Now().AddDate(-2, 0, 0)
	t.Run("app escreve na trilha mas não apaga nem trunca", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			if _, err := app.Exec(ctx,
				`INSERT INTO eventos_auditoria (ator_id, acao, alvo_tipo, alvo_id, ocorrido_em) VALUES (gen_random_uuid(), 'filme_criado', 'filme', $1, $2)`,
				fmt.Sprint(i+1), antigo); err != nil {
				t.Fatalf("INSERT na trilha: %v", err)
			}
		}
		if n := contarInt(t, app, `SELECT count(*) FROM eventos_auditoria`); n != 3 {
			t.Fatalf("SELECT na trilha: %d", n)
		}
		_, err := app.Exec(ctx, `DELETE FROM eventos_auditoria`)
		esperarNegado(t, err, "DELETE da trilha pelo app")
		_, err = app.Exec(ctx, `TRUNCATE eventos_auditoria`)
		esperarNegado(t, err, "TRUNCATE da trilha pelo app")
		_, err = app.Exec(ctx, `CREATE TRIGGER intruso AFTER INSERT ON eventos_auditoria FOR EACH ROW EXECUTE FUNCTION eventos_auditoria_imutavel()`)
		esperarNegado(t, err, "CREATE TRIGGER na trilha pelo app")
	})

	t.Run("purge roda auditoria.Purgar, mas não atualiza a trilha nem lê pedidos", func(t *testing.T) {
		_, err := purge.Exec(ctx, `UPDATE eventos_auditoria SET alvo_id = '9'`)
		esperarNegado(t, err, "UPDATE da trilha pelo purge")
		_, err = purge.Exec(ctx, `INSERT INTO eventos_auditoria (ator_id, acao, alvo_tipo, alvo_id, ocorrido_em) VALUES (gen_random_uuid(), 'filme_criado', 'filme', '1', now())`)
		esperarNegado(t, err, "INSERT na trilha pelo purge")
		_, err = purge.Exec(ctx, `SELECT 1 FROM pedidos`)
		esperarNegado(t, err, "SELECT em pedidos pelo purge")

		n, err := auditoria.Purgar(ctx, purge, time.Now().AddDate(-1, 0, 0), 2)
		if err != nil || n != 3 {
			t.Fatalf("Purgar = %d, %v; esperado 3 (em lotes de 2)", n, err)
		}
		if c := contarInt(t, super, `SELECT count(*) FROM eventos_auditoria`); c != 0 {
			t.Errorf("trilha com %d linhas após a purga", c)
		}
	})

	t.Run("backup lê tudo e não escreve", func(t *testing.T) {
		for _, tabela := range []string{"pedidos", "usuario", "eventos_auditoria", "outbox_events"} {
			if _, err := backup.Exec(ctx, "SELECT count(*) FROM "+tabela); err != nil {
				t.Errorf("SELECT em %s pelo backup: %v", tabela, err)
			}
		}
		_, err := backup.Exec(ctx, `INSERT INTO salas (nome, layout) VALUES ('X', '{}')`)
		esperarNegado(t, err, "INSERT pelo backup")
		_, err = backup.Exec(ctx, `DELETE FROM salas`)
		esperarNegado(t, err, "DELETE pelo backup")
		_, err = backup.Exec(ctx, `CREATE TABLE intruso (id int)`)
		esperarNegado(t, err, "CREATE TABLE pelo backup")
	})

	t.Run("018 é idempotente e o down devolve os privilégios ao app", func(t *testing.T) {
		migr := abrirPool(t, urlDoRole(host, porta, "morfeu_migrator", senhaMigr))
		sql := lerArquivo(t, "../../migrations/018_privilegios.up.sql")
		if _, err := migr.Exec(ctx, sql); err != nil {
			t.Fatalf("018 repetida: %v", err)
		}
		if err := m.Steps(-1); err != nil {
			t.Fatalf("down da 018: %v", err)
		}
		if _, err := app.Exec(ctx, `DELETE FROM eventos_auditoria`); err != nil {
			t.Errorf("após o down, o app deveria poder DELETE: %v", err)
		}
		if err := m.Steps(1); err != nil {
			t.Fatalf("up da 018: %v", err)
		}
		_, err := app.Exec(ctx, `DELETE FROM eventos_auditoria`)
		esperarNegado(t, err, "DELETE após reaplicar a 018")
	})
}

// TestMigration018SemRoles: sem os roles (dev/CI) a 018 é no-op e o usuário
// único segue podendo tudo.
func TestMigration018SemRoles(t *testing.T) {
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": bancoRoles},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })
	host, _ := pg.Host(ctx)
	p, _ := pg.MappedPort(ctx, "5432/tcp")

	m, err := migrate.New("file://../../migrations", urlDoRole(host, p.Port(), "postgres", "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil {
		t.Fatalf("migrations sem os roles: %v", err)
	}
	pool := abrirPool(t, urlDoRole(host, p.Port(), "postgres", "postgres"))
	if _, err := pool.Exec(ctx, `DELETE FROM eventos_auditoria`); err != nil {
		t.Errorf("usuário único deveria seguir com DELETE: %v", err)
	}
}

// TestRedisComSenha cobre CA05 com o `command:` REAL do docker-compose.prod.yml.
func TestRedisComSenha(t *testing.T) {
	ctx := context.Background()
	const senha = "senha-redis-so-de-teste-0123456789"

	bruto := lerArquivo(t, caminhoComposeProd)
	svc := lerCompose(t, []byte(bruto)).Services["redis"]
	var cmd []string
	for _, a := range svc["command"].([]any) {
		// No compose, $$ é um $ literal.
		cmd = append(cmd, strings.ReplaceAll(fmt.Sprint(a), "$$", "$"))
	}

	rd, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			Env:          map[string]string{"REDIS_PASSWORD": senha},
			Cmd:          cmd,
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("subir redis: %v", err)
	}
	t.Cleanup(func() { _ = rd.Terminate(ctx) })
	host, _ := rd.Host(ctx)
	p, _ := rd.MappedPort(ctx, "6379/tcp")
	addr := host + ":" + p.Port()

	sem := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = sem.Close() })
	if err := sem.Ping(ctx).Err(); err == nil || !strings.Contains(err.Error(), "NOAUTH") {
		t.Errorf("sem senha esperado NOAUTH, veio %v", err)
	}

	opt, err := redis.ParseURL(fmt.Sprintf("redis://:%s@%s/0", senha, addr))
	if err != nil {
		t.Fatal(err)
	}
	com := redis.NewClient(opt)
	t.Cleanup(func() { _ = com.Close() })
	if pong, err := com.Ping(ctx).Result(); err != nil || pong != "PONG" {
		t.Errorf("com a URL esperado PONG, veio %q, %v", pong, err)
	}

	// O healthcheck do compose (REDISCLI_AUTH) também precisa passar dentro do container.
	hc, _ := svc["healthcheck"].(map[string]any)
	teste := hc["test"].([]any)
	code, _, err := rd.Exec(ctx, []string{"sh", "-c", strings.ReplaceAll(fmt.Sprint(teste[1]), "$$", "$")})
	if err != nil || code != 0 {
		t.Errorf("healthcheck do compose: código %d, %v", code, err)
	}
}

func lerArquivo(t *testing.T, caminho string) string {
	t.Helper()
	b, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
