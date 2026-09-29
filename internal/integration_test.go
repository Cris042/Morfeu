//go:build integration
// +build integration

package internal

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/catalogo"
	"github.com/mclovin137/morfeu/internal/catalogo/db"
)

// setupTestDB creates an ephemeral PostgreSQL test container
func setupTestDB(t *testing.T, ctx context.Context) (string, testcontainers.Container) {
	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "morfeu_test",
		},
		// O log aparece duas vezes (initdb + processo final) — esperar a 2ª ocorrência
		// evita conectar durante o restart interno do initdb.
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(120 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start DB container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get DB host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get DB port: %v", err)
	}

	dsn := "postgres://postgres:postgres@" + host + ":" + port.Port() + "/morfeu_test"
	return dsn, container
}

// setupTestRedis creates an ephemeral Redis test container
func setupTestRedis(t *testing.T, ctx context.Context) (string, testcontainers.Container) {
	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(90 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start Redis container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get Redis host: %v", err)
	}

	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get Redis port: %v", err)
	}

	redisURL := "redis://" + host + ":" + port.Port()
	return redisURL, container
}

// initTestSchema aplica os arquivos de migration reais do catálogo (001 cria
// e semeia os 10 filmes; 007 leva ao modelo em PT) — task 0011: sem DDL
// duplicado no teste.
func initTestSchema(ctx context.Context, pool *pgxpool.Pool) error {
	for _, arq := range []string{"001_initial_schema.up.sql", "007_filmes.up.sql"} {
		ddl, err := os.ReadFile("../migrations/" + arq)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(ddl)); err != nil {
			return fmt.Errorf("aplicar %s: %w", arq, err)
		}
	}
	return nil
}

// seedTestData: os 10 filmes já vêm da migration 001 (initTestSchema).
func seedTestData(context.Context, *pgxpool.Pool) error { return nil }

// TestDatabaseConnection validates basic PostgreSQL connectivity
func TestDatabaseConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	dsn, container := setupTestDB(t, ctx)
	defer container.Terminate(ctx)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	err = pool.Ping(ctx)
	if err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}
}

// TestMigrationsUpDownUpIdempotent validates migrations can be run: up → down → up without error
func TestMigrationsUpDownUpIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	dsn, container := setupTestDB(t, ctx)
	defer container.Terminate(ctx)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	// Simulate "up" by creating schema
	if err := initTestSchema(ctx, pool); err != nil {
		t.Fatalf("Failed to create schema (up): %v", err)
	}

	// Seed data
	if err := seedTestData(ctx, pool); err != nil {
		t.Fatalf("Failed to seed data: %v", err)
	}

	// Verify films exist
	queries := db.New(pool)
	films, err := queries.ListarFilmesPublicos(ctx)
	if err != nil {
		t.Fatalf("Failed to list films after up: %v", err)
	}
	if len(films) != 10 {
		t.Errorf("Expected 10 films after up, got %d", len(films))
	}

	// Simulate "down"
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS filmes"); err != nil {
		t.Fatalf("Failed to drop table (down): %v", err)
	}

	// Verify table is gone
	var exists bool
	err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT FROM information_schema.tables WHERE table_name='filmes')").Scan(&exists)
	if err != nil {
		t.Fatalf("Failed to check table existence: %v", err)
	}
	if exists {
		t.Error("Expected films table to be dropped, but it exists")
	}

	// Simulate "up" again
	if err := initTestSchema(ctx, pool); err != nil {
		t.Fatalf("Failed to recreate schema (up again): %v", err)
	}

	if err := seedTestData(ctx, pool); err != nil {
		t.Fatalf("Failed to reseed data: %v", err)
	}

	// Verify films exist again
	films, err = queries.ListarFilmesPublicos(ctx)
	if err != nil {
		t.Fatalf("Failed to list films after up again: %v", err)
	}
	if len(films) != 10 {
		t.Errorf("Expected 10 films after up again, got %d", len(films))
	}
}

// TestCacheHitMissWithRedis validates cache hit/miss behavior with testcontainers Redis
func TestCacheHitMissWithRedis(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start DB container
	dbDSN, dbContainer := setupTestDB(t, ctx)
	defer dbContainer.Terminate(ctx)

	// Start Redis container
	redisURL, redisContainer := setupTestRedis(t, ctx)
	defer redisContainer.Terminate(ctx)

	// Setup database
	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	if err := initTestSchema(ctx, pool); err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}
	if err := seedTestData(ctx, pool); err != nil {
		t.Fatalf("Failed to seed data: %v", err)
	}

	// Setup Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisURL[8:], // Strip "redis://"
	})
	defer redisClient.Close()

	cacheLayer := cache.NewRedisCache(redisClient, zap.NewNop())
	svc := catalogo.NovoServico(db.New(pool), pool, cacheLayer, zap.NewNop())

	// First request should miss cache and hit database
	films1, err := svc.ListarPublicos(ctx)
	if err != nil {
		t.Fatalf("First ListFilms failed: %v", err)
	}
	if len(films1) != 10 {
		t.Errorf("Expected 10 films from DB, got %d", len(films1))
	}

	// Second request should hit cache
	films2, err := svc.ListarPublicos(ctx)
	if err != nil {
		t.Fatalf("Second ListFilms failed: %v", err)
	}
	if len(films2) != 10 {
		t.Errorf("Expected 10 films from cache, got %d", len(films2))
	}

	// Verify cache entry exists
	cacheVal, err := redisClient.Get(ctx, "catalogo:filmes:publicos").Result()
	if err != nil {
		t.Errorf("Cache miss: key 'catalogo:filmes:publicos' not found: %v", err)
	}
	if cacheVal == "" {
		t.Error("Cache entry is empty")
	}
}

// TestGracefulDegradationRedisUnavailable validates fallback when Redis is down
func TestGracefulDegradationRedisUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start DB container only
	dbDSN, dbContainer := setupTestDB(t, ctx)
	defer dbContainer.Terminate(ctx)

	// Setup database
	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	if err := initTestSchema(ctx, pool); err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}
	if err := seedTestData(ctx, pool); err != nil {
		t.Fatalf("Failed to seed data: %v", err)
	}

	// Use a Redis client that will fail (unreachable address)
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:9999", // Unreachable port
	})

	cacheLayer := cache.NewRedisCache(redisClient, zap.NewNop())
	svc := catalogo.NovoServico(db.New(pool), pool, cacheLayer, zap.NewNop())

	// Should fall back to database
	films, err := svc.ListarPublicos(ctx)
	if err != nil {
		t.Fatalf("ListFilms failed even with DB fallback: %v", err)
	}
	if len(films) != 10 {
		t.Errorf("Expected 10 films from DB fallback, got %d", len(films))
	}
}

// TestListFilmsE2E_FullStack validates end-to-end flow: handler → service → cache → db
func TestListFilmsE2E_FullStack(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start DB container
	dbDSN, dbContainer := setupTestDB(t, ctx)
	defer dbContainer.Terminate(ctx)

	// Start Redis container
	redisURL, redisContainer := setupTestRedis(t, ctx)
	defer redisContainer.Terminate(ctx)

	// Setup database
	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	if err := initTestSchema(ctx, pool); err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}
	if err := seedTestData(ctx, pool); err != nil {
		t.Fatalf("Failed to seed data: %v", err)
	}

	// Setup Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisURL[8:], // Strip "redis://"
	})
	defer redisClient.Close()

	cacheLayer := cache.NewRedisCache(redisClient, zap.NewNop())
	svc := catalogo.NovoServico(db.New(pool), pool, cacheLayer, zap.NewNop())

	// Full stack test: cache miss → database
	films, err := svc.ListarPublicos(ctx)
	if err != nil {
		t.Fatalf("E2E ListFilms failed: %v", err)
	}
	if len(films) != 10 {
		t.Errorf("Expected 10 films, got %d", len(films))
	}

	// Verify first film data
	// Ordem do cartaz agora é definida (criado_em DESC, id DESC — task 0011);
	// os seeds têm o mesmo criado_em, então o assert é por presença.
	achou := false
	for _, f := range films {
		if f.Titulo == "The Shawshank Redemption" && f.Ano != nil && *f.Ano == 1994 {
			achou = true
		}
	}
	if !achou {
		t.Errorf("Expected 'The Shawshank Redemption' (1994) in the listing, got %+v", films)
	}
}
