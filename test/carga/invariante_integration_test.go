//go:build integration
// +build integration

// Package carga_test prova que deploy/carga/invariante.sql acusa cada uma das
// três violações do invariante de assento (PRD 0045 CA07) e fica mudo num
// banco íntegro. O banco real impede (a) e (b) por índice único; o teste os
// derruba para plantar a violação — é a query que está sob prova.
package carga_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "morfeu_carga"},
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
		if pool, err = pgxpool.New(ctx, fmt.Sprintf("postgres://postgres:postgres@%s:%s/morfeu_carga?sslmode=disable", host, porta.Port())); err != nil {
			fmt.Fprintln(os.Stderr, "pool:", err)
			return 1
		}
		defer pool.Close()
		if err := aplicarMigrations(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

func aplicarMigrations(ctx context.Context) error {
	arqs, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(arqs)
	for _, a := range arqs {
		ddl, err := os.ReadFile(a) //nolint:gosec // caminhos fixos do repositório
		if err == nil {
			_, err = pool.Exec(ctx, string(ddl))
		}
		if err != nil {
			return fmt.Errorf("migration %s: %w", a, err)
		}
	}
	return nil
}

// violacoes roda o invariante.sql real e devolve só a coluna "violacao".
func violacoes(t *testing.T) []string {
	t.Helper()
	sql, err := os.ReadFile("../../deploy/carga/invariante.sql")
	if err != nil {
		t.Fatal(err)
	}
	linhas, err := pool.Query(context.Background(), string(sql))
	if err != nil {
		t.Fatalf("invariante.sql: %v", err)
	}
	defer linhas.Close()
	var out []string
	for linhas.Next() {
		var v, ref string
		var assento *string
		var n int64
		if err := linhas.Scan(&v, &ref, &assento, &n); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := linhas.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// semear cria o mínimo de FKs: filme 1 (migrations), sala e sessão.
func semear(t *testing.T) int64 {
	t.Helper()
	exec(t, `TRUNCATE ingressos, pedido_eventos, pedidos, holds RESTART IDENTITY CASCADE`)
	exec(t, `DELETE FROM sessoes`)
	exec(t, `INSERT INTO salas (nome, layout) VALUES ('Sala invariante', '{"fileiras":2,"colunas":2}') ON CONFLICT (nome) DO NOTHING`)
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO sessoes (filme_id, sala_id, inicio, duracao_min, fim, preco_centavos)
		SELECT 1, id, now() + interval '1 day', 100, now() + interval '1 day 2 hours', 3200 FROM salas WHERE nome = 'Sala invariante'
		RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

const hash32 = `decode(repeat('00', 32), 'hex')`

func TestInvariante_BancoIntegroNaoAcusaNada(t *testing.T) {
	sessao := semear(t)
	// Dados legítimos: um hold ativo, um convertido em outro assento e um
	// pedido pago com ingresso.
	exec(t, `INSERT INTO holds (id, sessao_id, assento_codigo, dono_hash, status, expires_at, criado_em, atualizado_em)
		VALUES (gen_random_uuid(), $1, 'A1', `+hash32+`, 'ativo', now() + interval '5 minutes', now(), now()),
		       (gen_random_uuid(), $1, 'A2', `+hash32+`, 'convertido', now(), now(), now())`, sessao)
	exec(t, `INSERT INTO pedidos (id, codigo, email, dono_hash, sessao_id, assentos, total_centavos, status, expira_em, criado_em, atualizado_em)
		VALUES ('00000000-0000-0000-0000-000000000001', 'AAAAAAAAAAAAAAAA', 'a@example.test', `+hash32+`, $1, '{A2}', 3200, 'pago', now(), now(), now())`, sessao)
	exec(t, `INSERT INTO ingressos (id, pedido_id, sessao_id, assento_codigo, status, criado_em)
		VALUES (gen_random_uuid(), '00000000-0000-0000-0000-000000000001', $1, 'A2', 'ativo', now())`, sessao)
	if v := violacoes(t); len(v) != 0 {
		t.Fatalf("banco íntegro acusou violações: %v", v)
	}
}

func TestInvariante_AcusaCadaViolacaoPlantada(t *testing.T) {
	t.Run("assento com dois holds vivos", func(t *testing.T) {
		sessao := semear(t)
		exec(t, `DROP INDEX holds_assento_ocupado`)
		t.Cleanup(func() {
			exec(t, `DELETE FROM holds`)
			exec(t, `CREATE UNIQUE INDEX holds_assento_ocupado ON holds (sessao_id, assento_codigo) WHERE status IN ('ativo', 'convertido')`)
		})
		exec(t, `INSERT INTO holds (id, sessao_id, assento_codigo, dono_hash, status, expires_at, criado_em, atualizado_em)
			VALUES (gen_random_uuid(), $1, 'B1', `+hash32+`, 'ativo', now(), now(), now()),
			       (gen_random_uuid(), $1, 'B1', `+hash32+`, 'convertido', now(), now(), now())`, sessao)
		if v := violacoes(t); len(v) != 1 || v[0] != "assento_com_mais_de_um_hold_vivo" {
			t.Fatalf("violações = %v", v)
		}
	})

	t.Run("assento com dois ingressos ativos", func(t *testing.T) {
		sessao := semear(t)
		exec(t, `DROP INDEX ingressos_assento_ativo`)
		t.Cleanup(func() {
			exec(t, `DELETE FROM ingressos`)
			exec(t, `CREATE UNIQUE INDEX ingressos_assento_ativo ON ingressos (sessao_id, assento_codigo) WHERE status = 'ativo'`)
		})
		exec(t, `INSERT INTO pedidos (id, codigo, email, dono_hash, sessao_id, assentos, total_centavos, status, expira_em, criado_em, atualizado_em)
			VALUES ('00000000-0000-0000-0000-000000000002', 'BBBBBBBBBBBBBBBB', 'b@example.test', `+hash32+`, $1, '{B2}', 3200, 'pago', now(), now(), now())`, sessao)
		exec(t, `INSERT INTO ingressos (id, pedido_id, sessao_id, assento_codigo, status, criado_em)
			VALUES (gen_random_uuid(), '00000000-0000-0000-0000-000000000002', $1, 'B2', 'ativo', now()),
			       (gen_random_uuid(), '00000000-0000-0000-0000-000000000002', $1, 'B2', 'ativo', now())`, sessao)
		if v := violacoes(t); len(v) != 1 || v[0] != "assento_com_mais_de_um_ingresso_ativo" {
			t.Fatalf("violações = %v", v)
		}
	})

	t.Run("pedido pago sem ingresso", func(t *testing.T) {
		sessao := semear(t)
		exec(t, `INSERT INTO pedidos (id, codigo, email, dono_hash, sessao_id, assentos, total_centavos, status, expira_em, criado_em, atualizado_em)
			VALUES ('00000000-0000-0000-0000-000000000003', 'CCCCCCCCCCCCCCCC', 'c@example.test', `+hash32+`, $1, '{C1}', 3200, 'pago', now(), now(), now())`, sessao)
		if v := violacoes(t); len(v) != 1 || v[0] != "pedido_pago_sem_ingresso" {
			t.Fatalf("violações = %v", v)
		}
	})
}
