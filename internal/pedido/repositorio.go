package pedido

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/mclovin137/morfeu/internal/pedido/db"
)

// repositorio é o repository manual do aggregate (ADR 0005): participa da TX
// de quem o criou (db.New(tx)). Struct concreta — há uma implementação só.
type repositorio struct {
	q *db.Queries
}

// inserir grava o pedido novo; ok=false = o carrinho já tem pedido pendente
// (índice único parcial decide, sem pré-check).
func (r repositorio) inserir(ctx context.Context, p Pedido, agora time.Time) (bool, error) {
	ids, err := r.q.InserirPedido(ctx, db.InserirPedidoParams{
		ID: p.id, Codigo: p.codigo, Email: p.email, UsuarioID: p.usuarioID, DonoHash: p.donoHash, SessaoID: p.sessaoID,
		Assentos: p.assentos, TotalCentavos: p.totalCentavos, ExpiraEm: p.expiraEm, Agora: agora,
	})
	if err != nil {
		return false, fmt.Errorf("pedido: inserir: %w", err)
	}
	if len(ids) == 0 {
		return false, nil
	}
	return true, r.registrar(ctx, p.id, nil, AguardandoPagamento, agora)
}

type pendente struct {
	id       uuid.UUID
	expiraEm time.Time
	intencao *string
}

func (r repositorio) pendenteDoDono(ctx context.Context, donoHash []byte) ([]pendente, error) {
	linhas, err := r.q.PendenteDoDono(ctx, donoHash)
	if err != nil {
		return nil, fmt.Errorf("pedido: pendente do dono: %w", err)
	}
	out := make([]pendente, 0, len(linhas))
	for _, l := range linhas {
		out = append(out, pendente{id: l.ID, expiraEm: l.ExpiraEm, intencao: l.PaymentIntentID})
	}
	return out, nil
}

// transicionar aplica o evento: a máquina em memória decide o destino e o
// compare-and-swap no SQL garante que só um caminho vence (ADR 0010). A
// transição e o registro na trilha acontecem na mesma TX.
func (r repositorio) transicionar(ctx context.Context, id uuid.UUID, de Status, ev Evento, agora time.Time) (Status, error) {
	para, err := Transicionar(de, ev)
	if err != nil {
		return "", err
	}
	n, err := r.q.Transicionar(ctx, db.TransicionarParams{Para: string(para), Agora: agora, ID: id, De: string(de)})
	if err != nil {
		return "", fmt.Errorf("pedido: transicionar: %w", err)
	}
	if n == 0 {
		return "", ErrTransicaoConcorrente
	}
	return para, r.registrar(ctx, id, &de, para, agora)
}

// marcarEstorno leva o pedido a estorno_pendente com o motivo, pelo mesmo CAS
// e na mesma TX da trilha (o estorno em si é o job da task 0025).
func (r repositorio) marcarEstorno(ctx context.Context, id uuid.UUID, de Status, motivo string, agora time.Time) error {
	para, err := Transicionar(de, EstornoNecessario)
	if err != nil {
		return err
	}
	n, err := r.q.MarcarEstorno(ctx, db.MarcarEstornoParams{Motivo: &motivo, Agora: agora, ID: id, De: string(de)})
	if err != nil {
		return fmt.Errorf("pedido: marcar estorno: %w", err)
	}
	if n == 0 {
		return ErrTransicaoConcorrente
	}
	return r.registrar(ctx, id, &de, para, agora)
}

func (r repositorio) registrar(ctx context.Context, id uuid.UUID, de *Status, para Status, agora time.Time) error {
	var deTexto *string
	if de != nil {
		s := string(*de)
		deTexto = &s
	}
	if err := r.q.RegistrarEvento(ctx, db.RegistrarEventoParams{PedidoID: id, De: deTexto, Para: string(para), Agora: agora}); err != nil {
		return fmt.Errorf("pedido: registrar evento: %w", err)
	}
	return nil
}

func (r repositorio) definirCobranca(ctx context.Context, id uuid.UUID, intencao string, agora time.Time) error {
	if _, err := r.q.DefinirCobranca(ctx, db.DefinirCobrancaParams{PaymentIntentID: &intencao, Agora: agora, ID: id}); err != nil {
		return fmt.Errorf("pedido: definir cobrança: %w", err)
	}
	return nil
}

// Visao é o pedido como o dono o enxerga (GET /pedidos/{id}).
type Visao struct {
	ID            uuid.UUID
	Codigo        string
	SessaoID      int64
	Assentos      []string
	TotalCentavos int64
	Status        Status
	ExpiraEm      time.Time
}

func (r repositorio) doDono(ctx context.Context, id uuid.UUID, donoHash []byte, usuarioID *uuid.UUID) (Visao, bool, error) {
	linhas, err := r.q.BuscarPedidoDoDono(ctx, db.BuscarPedidoDoDonoParams{ID: id, DonoHash: donoHash, UsuarioID: usuarioID})
	if err != nil {
		return Visao{}, false, fmt.Errorf("pedido: buscar: %w", err)
	}
	if len(linhas) == 0 {
		return Visao{}, false, nil
	}
	l := linhas[0]
	return Visao{ID: l.ID, Codigo: l.Codigo, SessaoID: l.SessaoID, Assentos: l.Assentos,
		TotalCentavos: l.TotalCentavos, Status: Status(l.Status), ExpiraEm: l.ExpiraEm}, true, nil
}

func (r repositorio) doUsuario(ctx context.Context, usuarioID uuid.UUID, limite, deslocamento int32) ([]Visao, error) {
	linhas, err := r.q.ListarPedidosDoUsuario(ctx, db.ListarPedidosDoUsuarioParams{UsuarioID: &usuarioID, Limite: limite, Deslocamento: deslocamento})
	if err != nil {
		return nil, fmt.Errorf("pedido: listar da conta: %w", err)
	}
	out := make([]Visao, 0, len(linhas))
	for _, l := range linhas {
		out = append(out, Visao{ID: l.ID, Codigo: l.Codigo, SessaoID: l.SessaoID, Assentos: l.Assentos,
			TotalCentavos: l.TotalCentavos, Status: Status(l.Status), ExpiraEm: l.ExpiraEm})
	}
	return out, nil
}
