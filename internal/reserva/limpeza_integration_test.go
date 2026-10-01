//go:build integration
// +build integration

package reserva

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Limpeza de holds terminais (PRD 0044). Reusa o PG e o relógio injetado da
// suíte (handler_test.go): nenhum teste depende de sleep ou do relógio real.

// semearHold insere um hold direto no banco (dono_hash de 32 bytes).
func semearHold(t *testing.T, sessaoID int64, assento, status string, atualizadoEm time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `INSERT INTO holds
		(id, sessao_id, assento_codigo, dono_hash, status, expires_at, criado_em, atualizado_em)
		VALUES ($1, $2, $3, decode(repeat('ab', 32), 'hex'), $4, $5, $5, $5)`,
		id, sessaoID, assento, status, atualizadoEm)
	if err != nil {
		t.Fatalf("semear hold %s/%s: %v", assento, status, err)
	}
	return id
}

func existeHold(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM holds WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("contar hold: %v", err)
	}
	return n == 1
}

// TestLimparHoldsTerminais cobre CA02: bordas por idade e status, idempotência.
func TestLimparHoldsTerminais(t *testing.T) {
	ctx := context.Background()
	sessaoID := novaSessao(t)
	corte := time.Date(2098, 1, 1, 12, 0, 0, 0, time.UTC).Add(-RetencaoHoldsTerminais)

	velho := corte.Add(-time.Second)
	recente := corte.Add(time.Second)
	apagados := []uuid.UUID{
		semearHold(t, sessaoID, "A1", "liberado", velho),
		semearHold(t, sessaoID, "A2", "expirado", velho),
		semearHold(t, sessaoID, "A3", "liberado", corte.Add(-365*24*time.Hour)),
	}
	ficam := []uuid.UUID{
		semearHold(t, sessaoID, "B1", "liberado", recente),
		semearHold(t, sessaoID, "B2", "expirado", recente),
		semearHold(t, sessaoID, "B3", "liberado", corte), // na borda exata: fica
		semearHold(t, sessaoID, "B4", "ativo", velho),
		semearHold(t, sessaoID, "B5", "convertido", velho),
		semearHold(t, sessaoID, "B6", "convertido", corte.Add(-365*24*time.Hour)),
	}

	n, err := LimparHoldsTerminais(ctx, pool, corte)
	if err != nil || n != int64(len(apagados)) {
		t.Fatalf("1ª execução: n=%d err=%v (esperado %d)", n, err, len(apagados))
	}
	for _, id := range apagados {
		if existeHold(t, id) {
			t.Errorf("hold terminal vencido %s deveria ter sido apagado", id)
		}
	}
	for _, id := range ficam {
		if !existeHold(t, id) {
			t.Errorf("hold %s não podia ser apagado", id)
		}
	}
	if n, err := LimparHoldsTerminais(ctx, pool, corte); err != nil || n != 0 {
		t.Fatalf("2ª execução deveria apagar 0: n=%d err=%v", n, err)
	}
}

// TestLimparHoldsTerminais_Lotes: mais de um lote é apagado por inteiro.
func TestLimparHoldsTerminais_Lotes(t *testing.T) {
	ctx := context.Background()
	sessaoID := novaSessao(t)
	corte := time.Date(2098, 1, 1, 12, 0, 0, 0, time.UTC).Add(-RetencaoHoldsTerminais)
	const total = 2*loteLimpezaHolds + 345
	_, err := pool.Exec(ctx, `INSERT INTO holds
		(id, sessao_id, assento_codigo, dono_hash, status, expires_at, criado_em, atualizado_em)
		SELECT gen_random_uuid(), $1, 'D1', decode(repeat('cd', 32), 'hex'),
		       CASE WHEN g % 2 = 0 THEN 'liberado' ELSE 'expirado' END, $2, $2, $2
		FROM generate_series(1, $3) g`, sessaoID, corte.Add(-time.Hour), total)
	if err != nil {
		t.Fatal(err)
	}
	vivo := semearHold(t, sessaoID, "D2", "ativo", corte.Add(-time.Hour))

	n, err := LimparHoldsTerminais(ctx, pool, corte)
	if err != nil || n != total {
		t.Fatalf("lotes: n=%d err=%v (esperado %d)", n, err, total)
	}
	if !existeHold(t, vivo) {
		t.Fatal("hold ativo apagado")
	}
	if n, err := LimparHoldsTerminais(ctx, pool, corte); err != nil || n != 0 {
		t.Fatalf("2ª execução: n=%d err=%v", n, err)
	}
}

// TestLimparHoldsTerminais_Concorrencia cobre CA03: limpeza × sweeper × reservas
// novas ao mesmo tempo não perdem hold vivo nem falham.
func TestLimparHoldsTerminais_Concorrencia(t *testing.T) {
	ctx := context.Background()
	a := novoAmbiente(t)
	sessaoID := novaSessao(t)
	vencido := holdsDe(t, a.travar(sessaoID, novoDono(t), "A2"))[0]
	a.rel.avancar(TTLHold) // A2 vence; A3, criado depois, está vivo
	vivo := holdsDe(t, a.travar(sessaoID, novoDono(t), "A3"))[0]
	corte := a.rel.agora().Add(-RetencaoHoldsTerminais)
	for i := range 300 {
		semearHold(t, sessaoID, "E1", map[bool]string{true: "liberado", false: "expirado"}[i%2 == 0], corte.Add(-time.Hour))
	}

	const rodadas = 5
	donos := novosDonos(t, rodadas)
	assentos := []string{"B1", "B2", "B3", "B4", "B5"}
	largada := make(chan struct{})
	var wg sync.WaitGroup
	erros := make(chan error, 3*rodadas)
	respostas := make([]resposta, rodadas)
	for i := range rodadas {
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-largada
			if _, err := LimparHoldsTerminais(ctx, pool, corte); err != nil {
				erros <- err
			}
		}()
		go func() {
			defer wg.Done()
			<-largada
			if _, err := a.sweeper.VarrerExpirados(ctx); err != nil {
				erros <- err
			}
		}()
		go func() {
			defer wg.Done()
			<-largada
			respostas[i] = a.travar(sessaoID, donos[i], assentos[i])
		}()
	}
	close(largada)
	wg.Wait()
	close(erros)
	for err := range erros {
		t.Errorf("rotina concorrente falhou: %v", err)
	}
	for i, r := range respostas {
		if r.code != http.StatusCreated {
			t.Errorf("reserva %d: %d %s", i, r.code, r.corpo)
		}
		if n, _ := vivos(t, sessaoID, assentos[i], a.rel.agora()); n != 1 {
			t.Errorf("assento %s: %d vivos (esperado 1)", assentos[i], n)
		}
	}
	if n, _ := vivos(t, sessaoID, "A3", a.rel.agora()); n != 1 || !existeHold(t, vivo.ID) {
		t.Fatalf("hold vivo perdido: vivos=%d", n)
	}
	// O vencido (A2) foi expirado pelo sweeper, mas é recente: a limpeza não o apaga.
	if !existeHold(t, vencido.ID) {
		t.Fatal("hold expirado recente apagado")
	}
	var antigos int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM holds WHERE sessao_id = $1 AND assento_codigo = 'E1'`, sessaoID).Scan(&antigos); err != nil || antigos != 0 {
		t.Fatalf("terminais antigos restantes: %d err=%v", antigos, err)
	}
}
