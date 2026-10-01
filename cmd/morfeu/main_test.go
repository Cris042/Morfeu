//go:build integration
// +build integration

package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Suíte de integração do subcomando `morfeu criar-filme` (RF02/CA05):
// exercita a função runCriarFilme diretamente (não o binário compilado, per
// o plano de testes do PRD 0002) sobre um PG efêmero próprio deste pacote
// (container por pacote, RNF05/ADR 0006). Não depende de RabbitMQ: Enqueue
// só grava na outbox, a publicação é responsabilidade do relay (fora desta
// suíte).
var testDatabaseURL string

func TestMain(m *testing.M) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "morfeu_cli_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(120 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "iniciar container postgres:", err)
		os.Exit(1)
	}

	host, err := container.Host(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "host do postgres:", err)
		os.Exit(1)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		fmt.Fprintln(os.Stderr, "porta do postgres:", err)
		os.Exit(1)
	}
	testDatabaseURL = "postgres://postgres:postgres@" + host + ":" + port.Port() + "/morfeu_cli_test?sslmode=disable"

	pool, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pool do postgres:", err)
		os.Exit(1)
	}
	// Schema pelos arquivos de migration reais (001 filmes-seed, 002 outbox,
	// 007 catálogo em PT) — sem DDL duplicado no teste (task 0011).
	for _, arq := range []string{"001_initial_schema.up.sql", "002_outbox_events.up.sql", "007_filmes.up.sql", "017_eventos_auditoria.up.sql"} {
		ddl, lerErr := os.ReadFile("../../migrations/" + arq)
		if lerErr != nil {
			fmt.Fprintln(os.Stderr, "ler migration:", lerErr)
			os.Exit(1)
		}
		if _, execErr := pool.Exec(ctx, string(ddl)); execErr != nil {
			fmt.Fprintln(os.Stderr, "aplicar migration", arq, execErr)
			os.Exit(1)
		}
	}
	pool.Close()

	setTestEnv()
	code := m.Run()
	unsetTestEnv()

	_ = container.Terminate(ctx)
	os.Exit(code)
}

func setTestEnv() {
	_ = os.Setenv("DATABASE_URL", testDatabaseURL)
	// runCriarFilme não conecta a Redis/RabbitMQ, mas config.Validate exige
	// as duas variáveis não vazias.
	_ = os.Setenv("REDIS_URL", "redis://localhost:6379/0")
	_ = os.Setenv("RABBITMQ_URL", "amqp://unused:unused@localhost:5672/")
}

func unsetTestEnv() {
	_ = os.Unsetenv("DATABASE_URL")
	_ = os.Unsetenv("REDIS_URL")
	_ = os.Unsetenv("RABBITMQ_URL")
}

// TestRunCriarFilme_TituloAusente cobre CA05: flags inválidas (-titulo
// ausente) retornam erro (exit code != 0 no main real) e nada persiste.
func TestRunCriarFilme_TituloAusente(t *testing.T) {
	if err := runCriarFilme([]string{"-sinopse=sem titulo"}); err == nil {
		t.Fatal("esperava erro com -titulo ausente")
	}
}

// TestRunCriarFilme_FlagsValidas cobre RF02/CA05: cria o filme e o evento na
// mesma TX via o service real, imprime o id (via CreateFilm) e retorna nil.
func TestRunCriarFilme_FlagsValidas(t *testing.T) {
	err := runCriarFilme([]string{
		"-titulo=Filme de Teste CLI",
		"-sinopse=Uma sinopse de teste",
		"-ano=2024",
		"-duracao=100",
	})
	if err != nil {
		t.Fatalf("runCriarFilme falhou com flags válidas: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), testDatabaseURL)
	if err != nil {
		t.Fatalf("abrir pool de verificação: %v", err)
	}
	defer pool.Close()

	var count int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM filmes WHERE titulo = $1", "Filme de Teste CLI",
	).Scan(&count); err != nil {
		t.Fatalf("verificar filme criado: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava 1 filme criado, encontrei %d", count)
	}

	var pendingEvents int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_events oe
		 JOIN filmes f ON f.id::text = oe.aggregate_id
		 WHERE f.titulo = $1 AND oe.event_type = 'catalogo.filme_criado'`,
		"Filme de Teste CLI",
	).Scan(&pendingEvents); err != nil {
		t.Fatalf("verificar evento enfileirado: %v", err)
	}
	if pendingEvents != 1 {
		t.Fatalf("esperava 1 evento catalogo.filme_criado enfileirado, encontrei %d", pendingEvents)
	}
}

// TestRunCriarFilme_SemDuracao: a duração passou a ser obrigatória (PRD 0011
// RF07 — o E3 depende dela para a regra de não-conflito).
func TestRunCriarFilme_SemDuracao(t *testing.T) {
	if err := runCriarFilme([]string{"-titulo=Sem Duração"}); err == nil {
		t.Fatal("esperava erro sem -duracao")
	}
}
