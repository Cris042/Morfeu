package reserva

import (
	"context"
	"fmt"
	"time"

	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/reserva/db"
)

// RetencaoHoldsTerminais: holds liberado/expirado são só histórico
// (PRD 0044); 7 dias bastam para investigar. ativo e convertido nunca saem.
const RetencaoHoldsTerminais = 7 * 24 * time.Hour

const loteLimpezaHolds = 1000

// LimparHoldsTerminais apaga holds liberado/expirado com atualizado_em
// anterior a antesDe, em lotes de 1000 (cada lote é um DELETE curto). Não
// toca ativo nem convertido (este ocupa o índice da trava — ADR 0008/0010).
// Devolve o total apagado.
func LimparHoldsTerminais(ctx context.Context, pool outbox.Pool, antesDe time.Time) (int64, error) {
	var total int64
	for ctx.Err() == nil {
		n, err := db.New(pool).LimparHoldsTerminais(ctx, db.LimparHoldsTerminaisParams{AntesDe: antesDe, Limite: loteLimpezaHolds})
		if err != nil {
			return total, fmt.Errorf("reserva: limpar holds terminais: %w", err)
		}
		total += n
		if n < loteLimpezaHolds {
			break
		}
	}
	return total, nil
}
