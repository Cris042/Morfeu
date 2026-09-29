//go:build integration
// +build integration

package autenticacao

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// redisReal sobe um Redis efêmero (ADR 0006) e devolve o cliente.
func redisReal(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if c != nil {
		t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	}
	if err != nil {
		t.Fatalf("iniciar redis: %v", err)
	}
	host, _ := c.Host(ctx)
	porta, _ := c.MappedPort(ctx, "6379/tcp")
	cli := redis.NewClient(&redis.Options{Addr: host + ":" + porta.Port()})
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

// TestLimitador_RedisReal cobre CA04: bloqueia na Max-ésima falha, a janela
// é um TTL real no Redis, a chave gravada é hash (sem o texto original) e
// Limpar remove a chave.
func TestLimitador_RedisReal(t *testing.T) {
	cli := redisReal(t)
	ctx := context.Background()
	l, err := NovoLimitador(ConfigLimitador{Redis: cli, Prefixo: "morfeu:login:", Max: 5, Janela: 5 * time.Minute}, zap.NewNop())
	if err != nil {
		t.Fatalf("NovoLimitador: %v", err)
	}
	chave := "conta:maria@exemplo.com"

	for i := 0; i < 5; i++ {
		if l.Bloqueado(ctx, chave) {
			t.Fatalf("bloqueado cedo demais na falha %d", i+1)
		}
		l.RegistrarFalha(ctx, chave)
	}
	if !l.Bloqueado(ctx, chave) {
		t.Fatal("deveria bloquear após 5 falhas")
	}

	gravadas, err := cli.Keys(ctx, "morfeu:login:*").Result()
	if err != nil || len(gravadas) != 1 {
		t.Fatalf("esperava 1 chave no Redis, veio %v (err=%v)", gravadas, err)
	}
	if strings.Contains(gravadas[0], "maria") || strings.Contains(gravadas[0], "@") {
		t.Errorf("chave do Redis vaza o original: %s", gravadas[0])
	}
	ttl, err := cli.PTTL(ctx, gravadas[0]).Result()
	if err != nil || ttl <= 0 || ttl > 5*time.Minute {
		t.Errorf("janela deveria ser TTL real em (0, 5min], veio %v (err=%v)", ttl, err)
	}

	// Reinícios de falha não estendem a janela (EXPIRE NX).
	l.RegistrarFalha(ctx, chave)
	ttl2, _ := cli.PTTL(ctx, gravadas[0]).Result()
	if ttl2 > ttl {
		t.Errorf("falha nova não pode estender a janela: %v > %v", ttl2, ttl)
	}

	l.Limpar(ctx, chave)
	if l.Bloqueado(ctx, chave) {
		t.Fatal("Limpar deveria liberar")
	}
	if n, _ := cli.Exists(ctx, gravadas[0]).Result(); n != 0 {
		t.Error("Limpar deveria apagar a chave do Redis")
	}
}

// TestLimitador_RedisIndisponivel cobre CA05: sem Redis, o comportamento é o
// mesmo (fallback em memória), sem erro ao chamador e com log warn que não
// contém a chave.
func TestLimitador_RedisIndisponivel(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	cli := redisInalcancavel()
	t.Cleanup(func() { _ = cli.Close() })
	l, err := NovoLimitador(ConfigLimitador{Redis: cli, Prefixo: "morfeu:login:", Max: 5, Janela: 5 * time.Minute}, zap.New(core))
	if err != nil {
		t.Fatalf("NovoLimitador: %v", err)
	}
	ctx := context.Background()
	chave := "conta:maria@exemplo.com"

	for i := 0; i < 5; i++ {
		l.RegistrarFalha(ctx, chave)
	}
	if !l.Bloqueado(ctx, chave) {
		t.Fatal("fallback em memória deveria bloquear após 5 falhas")
	}
	if logs.Len() == 0 {
		t.Fatal("degradação para memória deveria gerar log warn")
	}
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "maria") || strings.Contains(e.ContextMap()["error"].(string), "maria") {
			t.Errorf("log de fallback vaza a chave: %v", e)
		}
	}
}
