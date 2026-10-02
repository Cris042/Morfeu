//go:build integration
// +build integration

package outbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mclovin137/morfeu/internal/outbox"
)

// Limpeza do dedup (PRD 0044, CA01). O corte é passado por parâmetro: as
// bordas são testadas sem sleep. Cada teste usa um consumidor próprio.

func semearProcessada(t *testing.T, pool *pgxpool.Pool, consumidor string, processadaEm time.Time) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO processed_messages (message_id, consumidor, processed_at) VALUES ($1, $2, $3)`,
		id, consumidor, processadaEm); err != nil {
		t.Fatalf("semear processed_messages: %v", err)
	}
	return id
}

func restantes(t *testing.T, pool *pgxpool.Pool, consumidor string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM processed_messages WHERE consumidor = $1`, consumidor).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLimparProcessadas_Bordas(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	consumidor := "limpeza-bordas-" + uuid.NewString()
	corte := time.Now().Add(-outbox.JanelaDedup)

	velha := semearProcessada(t, pool, consumidor, corte.Add(-time.Second))
	naBorda := semearProcessada(t, pool, consumidor, corte)
	dentro := semearProcessada(t, pool, consumidor, corte.Add(time.Second))
	agora := semearProcessada(t, pool, consumidor, time.Now())

	n, err := outbox.LimparProcessadas(ctx, pool, corte)
	if err != nil || n != 1 {
		t.Fatalf("1ª execução: n=%d err=%v (esperado 1)", n, err)
	}
	var existe []string
	rows, err := pool.Query(ctx, `SELECT message_id FROM processed_messages WHERE consumidor = $1`, consumidor)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		existe = append(existe, id)
	}
	for _, id := range existe {
		if id == velha {
			t.Error("registro 1 s antes da janela deveria ter sido apagado")
		}
	}
	if len(existe) != 3 {
		t.Fatalf("restaram %v; esperado borda, dentro e agora (%s, %s, %s)", existe, naBorda, dentro, agora)
	}
	if n, err := outbox.LimparProcessadas(ctx, pool, corte); err != nil || n != 0 {
		t.Fatalf("2ª execução deveria apagar 0: n=%d err=%v", n, err)
	}
}

func TestLimparProcessadas_Lotes(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	consumidor := "limpeza-lotes-" + uuid.NewString()
	corte := time.Now().Add(-outbox.JanelaDedup)
	const total = 2345 // 3 lotes de 1000
	if _, err := pool.Exec(ctx, `INSERT INTO processed_messages (message_id, consumidor, processed_at)
		SELECT gen_random_uuid(), $1, $2 FROM generate_series(1, $3)`,
		consumidor, corte.Add(-time.Hour), total); err != nil {
		t.Fatal(err)
	}
	semearProcessada(t, pool, consumidor, time.Now())

	n, err := outbox.LimparProcessadas(ctx, pool, corte)
	// >=: linhas de borda de outros testes, já vencidas para este corte, também saem.
	if err != nil || n < total {
		t.Fatalf("lotes: n=%d err=%v (esperado >= %d)", n, err, total)
	}
	if r := restantes(t, pool, consumidor); r != 1 {
		t.Fatalf("restaram %d, esperado só o recente", r)
	}
	if n, err := outbox.LimparProcessadas(ctx, pool, corte); err != nil || n != 0 {
		t.Fatalf("2ª execução: n=%d err=%v", n, err)
	}
}
