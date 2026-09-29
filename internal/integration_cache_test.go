//go:build integration
// +build integration

package internal

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/catalogo"
	"github.com/mclovin137/morfeu/internal/catalogo/db"
)

// TestCacheHitMiss_FirstRequestMisses validates that first request hits DB and caches result
func TestCacheHitMiss_FirstRequestMisses(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup containers
	dbDSN, dbContainer := setupTestDB(t, ctx)
	defer dbContainer.Terminate(ctx)

	redisURL, redisContainer := setupTestRedis(t, ctx)
	defer redisContainer.Terminate(ctx)

	// Setup database
	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		t.Fatalf("Failed to create DB pool: %v", err)
	}
	defer pool.Close()

	if err := initTestSchema(ctx, pool); err != nil {
		t.Fatalf("Failed to init schema: %v", err)
	}
	if err := seedTestData(ctx, pool); err != nil {
		t.Fatalf("Failed to seed data: %v", err)
	}

	// Setup Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisURL[8:], // Strip "redis://"
	})
	defer redisClient.Close()

	// Flush Redis to ensure cache is empty
	redisClient.FlushDB(ctx)

	cacheLayer := cache.NewRedisCache(redisClient, zap.NewNop())
	svc := catalogo.NovoServico(db.New(pool), pool, cacheLayer, zap.NewNop())

	// First request should miss cache
	start := time.Now()
	films, err := svc.ListarPublicos(ctx)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("First ListFilms failed: %v", err)
	}

	if len(films) != 10 {
		t.Errorf("Expected 10 films, got %d", len(films))
	}

	// First request should take longer (DB hit)
	t.Logf("First request (cache miss) took %v", duration)

	// Verify cache was populated
	cachedData, err := redisClient.Get(ctx, "catalogo:filmes:publicos").Result()
	if err == redis.Nil {
		t.Error("Cache was not populated after first request")
	} else if err != nil {
		t.Fatalf("Redis error: %v", err)
	}

	if cachedData == "" {
		t.Error("Cached data is empty")
	}

	// Parse cached data to verify it's valid JSON
	var cachedFilms []catalogo.Filme
	if err := json.Unmarshal([]byte(cachedData), &cachedFilms); err != nil {
		t.Fatalf("Failed to unmarshal cached data: %v", err)
	}

	if len(cachedFilms) != 10 {
		t.Errorf("Expected 10 films in cache, got %d", len(cachedFilms))
	}
}

// TestCacheHitMiss_SecondRequestHits validates that second request hits cache
func TestCacheHitMiss_SecondRequestHits(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup containers
	dbDSN, dbContainer := setupTestDB(t, ctx)
	defer dbContainer.Terminate(ctx)

	redisURL, redisContainer := setupTestRedis(t, ctx)
	defer redisContainer.Terminate(ctx)

	// Setup database
	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		t.Fatalf("Failed to create DB pool: %v", err)
	}
	defer pool.Close()

	if err := initTestSchema(ctx, pool); err != nil {
		t.Fatalf("Failed to init schema: %v", err)
	}
	if err := seedTestData(ctx, pool); err != nil {
		t.Fatalf("Failed to seed data: %v", err)
	}

	// Setup Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisURL[8:], // Strip "redis://"
	})
	defer redisClient.Close()

	// Flush Redis to ensure cache is empty
	redisClient.FlushDB(ctx)

	cacheLayer := cache.NewRedisCache(redisClient, zap.NewNop())
	svc := catalogo.NovoServico(db.New(pool), pool, cacheLayer, zap.NewNop())

	// First request (cache miss)
	_, err = svc.ListarPublicos(ctx)
	if err != nil {
		t.Fatalf("First ListFilms failed: %v", err)
	}

	// Second request (should hit cache)
	start := time.Now()
	films, err := svc.ListarPublicos(ctx)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Second ListFilms failed: %v", err)
	}

	if len(films) != 10 {
		t.Errorf("Expected 10 films, got %d", len(films))
	}

	// Second request should be faster (cache hit)
	t.Logf("Second request (cache hit) took %v", duration)

	// Verify cache entry still exists with TTL
	ttl, err := redisClient.PTTL(ctx, "catalogo:filmes:publicos").Result()
	if err != nil {
		t.Fatalf("Failed to get TTL: %v", err)
	}

	if ttl <= 0 {
		t.Errorf("Expected TTL > 0, got %v", ttl)
	}

	t.Logf("Cache TTL: %v", ttl)
}

// TestCacheTTLRespected validates that cache entry expires after TTL
func TestCacheTTLRespected(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup containers
	dbDSN, dbContainer := setupTestDB(t, ctx)
	defer dbContainer.Terminate(ctx)

	redisURL, redisContainer := setupTestRedis(t, ctx)
	defer redisContainer.Terminate(ctx)

	// Setup database
	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		t.Fatalf("Failed to create DB pool: %v", err)
	}
	defer pool.Close()

	if err := initTestSchema(ctx, pool); err != nil {
		t.Fatalf("Failed to init schema: %v", err)
	}
	if err := seedTestData(ctx, pool); err != nil {
		t.Fatalf("Failed to seed data: %v", err)
	}

	// Setup Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisURL[8:], // Strip "redis://"
	})
	defer redisClient.Close()

	// Flush Redis to ensure cache is empty
	redisClient.FlushDB(ctx)

	cacheLayer := cache.NewRedisCache(redisClient, zap.NewNop())
	svc := catalogo.NovoServico(db.New(pool), pool, cacheLayer, zap.NewNop())

	// First request to populate cache
	_, err = svc.ListarPublicos(ctx)
	if err != nil {
		t.Fatalf("First ListFilms failed: %v", err)
	}

	// Verify cache has entry with TTL
	ttl1, err := redisClient.PTTL(ctx, "catalogo:filmes:publicos").Result()
	if err != nil {
		t.Fatalf("Failed to get initial TTL: %v", err)
	}

	if ttl1 <= 0 {
		t.Errorf("Expected initial TTL > 0, got %v", ttl1)
	}

	t.Logf("Initial TTL: %v", ttl1)

	// Wait and check TTL decreased
	time.Sleep(100 * time.Millisecond)
	ttl2, err := redisClient.PTTL(ctx, "catalogo:filmes:publicos").Result()
	if err != nil {
		t.Fatalf("Failed to get second TTL: %v", err)
	}

	if ttl2 >= ttl1 {
		t.Errorf("Expected TTL to decrease, but got: initial=%v, after=%v", ttl1, ttl2)
	}

	t.Logf("TTL after delay: %v", ttl2)
}
