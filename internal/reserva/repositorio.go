package reserva

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/mclovin137/morfeu/internal/reserva/db"
)

// Namespaces dos advisory locks do módulo (primeiro argumento do lock de
// duas chaves — não colidem com outros usos).
const (
	namespaceDono    int32 = 4001
	namespaceSweeper int32 = 4002
	loteSweeper            = 500
)

// repositorio é o repository manual do aggregate Hold (ADR 0005): mapeia
// sqlc ↔ domínio e participa da TX de quem o criou (db.New(tx)). Struct
// concreta — há uma única implementação.
type repositorio struct {
	q *db.Queries
}

func (r repositorio) travarDono(ctx context.Context, d Dono) error {
	if err := r.q.TravarDono(ctx, db.TravarDonoParams{Namespace: namespaceDono, Chave: d.chaveTrava()}); err != nil {
		return fmt.Errorf("reserva: travar dono: %w", err)
	}
	return nil
}

func (r repositorio) vivosDoDono(ctx context.Context, d Dono, agora time.Time) ([]Hold, error) {
	linhas, err := r.q.HoldsVivosDoDono(ctx, db.HoldsVivosDoDonoParams{DonoHash: d.hash, Agora: agora})
	if err != nil {
		return nil, fmt.Errorf("reserva: holds do dono: %w", err)
	}
	out := make([]Hold, 0, len(linhas))
	for _, l := range linhas {
		h := reconstituir(l.ID, l.SessaoID, l.AssentoCodigo, l.ExpiresAt, int(l.ExtensoesUsadas))
		h.emPedido = l.PedidoID != nil
		out = append(out, h)
	}
	return out, nil
}

// travar cria o hold ou rouba um vencido; ok=false = vivo de outro dono.
func (r repositorio) travar(ctx context.Context, sessaoID int64, a AssentoCodigo, d Dono, agora time.Time) (Hold, bool, error) {
	linhas, err := r.q.TravarAssento(ctx, db.TravarAssentoParams{
		ID: uuid.New(), SessaoID: sessaoID, AssentoCodigo: string(a), DonoHash: d.hash,
		ExpiresAt: agora.Add(TTLHold), Agora: agora,
	})
	if err != nil {
		return Hold{}, false, fmt.Errorf("reserva: travar assento: %w", err)
	}
	if len(linhas) == 0 {
		return Hold{}, false, nil
	}
	l := linhas[0]
	return reconstituir(l.ID, l.SessaoID, l.AssentoCodigo, l.ExpiresAt, int(l.ExtensoesUsadas)), true, nil
}

func (r repositorio) vivoDoDono(ctx context.Context, id uuid.UUID, d Dono, agora time.Time) (Hold, bool, error) {
	linhas, err := r.q.BuscarHoldVivoDoDono(ctx, db.BuscarHoldVivoDoDonoParams{ID: id, DonoHash: d.hash, Agora: agora})
	if err != nil {
		return Hold{}, false, fmt.Errorf("reserva: buscar hold: %w", err)
	}
	if len(linhas) == 0 {
		return Hold{}, false, nil
	}
	l := linhas[0]
	h := reconstituir(l.ID, l.SessaoID, l.AssentoCodigo, l.ExpiresAt, int(l.ExtensoesUsadas))
	h.emPedido = l.PedidoID != nil
	return h, true, nil
}

// estender persiste a extensão com a guarda extensoes_usadas = 0.
func (r repositorio) estender(ctx context.Context, h Hold, d Dono, agora time.Time) (bool, error) {
	linhas, err := r.q.EstenderHold(ctx, db.EstenderHoldParams{
		ID: h.id, DonoHash: d.hash, NovoExpiresAt: h.expiraEm, Agora: agora,
	})
	if err != nil {
		return false, fmt.Errorf("reserva: estender hold: %w", err)
	}
	return len(linhas) == 1, nil
}

func (r repositorio) liberar(ctx context.Context, id uuid.UUID, d Dono, agora time.Time) (bool, error) {
	n, err := r.q.LiberarHold(ctx, db.LiberarHoldParams{ID: id, DonoHash: d.hash, Agora: agora})
	if err != nil {
		return false, fmt.Errorf("reserva: liberar hold: %w", err)
	}
	return n == 1, nil
}
