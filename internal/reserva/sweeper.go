package reserva

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/reserva/db"
)

// IntervaloSweeper entre passadas (RF09). A correção não depende dele: o
// upsert rouba vencidos mesmo sem varredura (ADR 0008).
const IntervaloSweeper = time.Minute

// Sweeper marca holds vencidos como 'expirado' (higiene, RF09). Roda no
// worker; não precisa da porta de sessões nem dos limitadores da API.
type Sweeper struct {
	pool     outbox.Pool
	metricas Metricas
	agora    func() time.Time
	logger   *zap.Logger
}

// NovoSweeper cria o sweeper; agora nil = time.Now.
func NovoSweeper(pool outbox.Pool, m Metricas, agora func() time.Time, logger *zap.Logger) *Sweeper {
	if agora == nil {
		agora = time.Now
	}
	return &Sweeper{pool: pool, metricas: m.comPadroes(), agora: agora, logger: logger}
}

// VarrerExpirados faz uma passada (gatilho explícito nos testes): marca até
// 500 vencidos. Se outra instância já está varrendo, não faz nada.
func (s *Sweeper) VarrerExpirados(ctx context.Context) (int64, error) {
	var n int64
	err := outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		q := db.New(tx)
		obtido, err := q.TentarTravaSweeper(ctx, db.TentarTravaSweeperParams{Namespace: namespaceSweeper, Chave: 0})
		if err != nil {
			return err
		}
		if !obtido {
			return errTravaSweeperOcupada
		}
		n, err = q.ExpirarVencidos(ctx, db.ExpirarVencidosParams{Agora: s.agora(), Limite: loteSweeper})
		return err
	})
	if errors.Is(err, errTravaSweeperOcupada) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	s.metricas.Varreduras(ctx)
	s.metricas.Expirados(ctx, n)
	return n, nil
}

// Rodar varre a cada intervalo até ctx acabar.
func (s *Sweeper) Rodar(ctx context.Context, intervalo time.Duration) {
	ticker := time.NewTicker(intervalo)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.VarrerExpirados(ctx)
			if err != nil {
				s.logger.Error("reserva: sweeper falhou", zap.Error(err))
				continue
			}
			if n > 0 {
				s.logger.Info("reserva: holds expirados", zap.Int64("quantidade", n))
			}
		}
	}
}
