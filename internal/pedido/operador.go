package pedido

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/auditoria"
	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/pedido/db"
)

// Backoffice do pedido (PRD 0037): consulta e cancelamento individual pelo
// operador. O operador vê o e-mail MASCARADO e nunca o código do pedido (é a
// credencial da consulta do convidado — refinamento E9, security).

// TamanhoPaginaOperador: no máximo 50 pedidos por página.
const TamanhoPaginaOperador = 50

// FiltroOperador restringe a listagem (campos nil = sem filtro).
type FiltroOperador struct {
	SessaoID *int64
	Status   *Status
}

// PedidoOperador é o pedido como o operador o vê.
type PedidoOperador struct {
	ID             uuid.UUID
	SessaoID       int64
	EmailMascarado string
	Assentos       []string
	TotalCentavos  int64
	Status         Status
	MotivoEstorno  string
	CriadoEm       time.Time
}

// IngressoOperador é um ingresso do pedido (sem token nem link).
type IngressoOperador struct {
	Assento string
	Status  string
}

// EventoOperador é uma transição da trilha do pedido.
type EventoOperador struct {
	De, Para   string
	OcorridoEm time.Time
}

// DetalheOperador é o pedido com ingressos e trilha.
type DetalheOperador struct {
	PedidoOperador
	Ingressos []IngressoOperador
	Eventos   []EventoOperador
}

// mascararEmail mantém a 1ª letra e o domínio: "a***@exemplo.com".
func mascararEmail(e string) string {
	local, dominio, ok := strings.Cut(e, "@")
	if !ok || local == "" {
		return "***"
	}
	return local[:1] + "***@" + dominio
}

func paraOperador(id uuid.UUID, sessaoID int64, email string, assentos []string, total int64, status string, motivo *string, criado time.Time) PedidoOperador {
	p := PedidoOperador{ID: id, SessaoID: sessaoID, EmailMascarado: mascararEmail(email), Assentos: assentos,
		TotalCentavos: total, Status: Status(status), CriadoEm: criado}
	if motivo != nil {
		p.MotivoEstorno = *motivo
	}
	return p
}

// ListarParaOperador lista os pedidos com os filtros, mais recentes primeiro.
func (s *Servico) ListarParaOperador(ctx context.Context, f FiltroOperador, pagina int) ([]PedidoOperador, error) {
	if pagina < 1 || pagina > 1000 {
		pagina = 1
	}
	var status *string
	if f.Status != nil {
		st := string(*f.Status)
		status = &st
	}
	linhas, err := db.New(s.pool).ListarPedidosOperador(ctx, db.ListarPedidosOperadorParams{
		SessaoID: f.SessaoID, Status: status, Limite: TamanhoPaginaOperador,
		Deslocamento: int32((pagina - 1) * TamanhoPaginaOperador), //nolint:gosec // página limitada acima
	})
	if err != nil {
		return nil, fmt.Errorf("pedido: listar para o operador: %w", err)
	}
	out := make([]PedidoOperador, 0, len(linhas))
	for _, l := range linhas {
		out = append(out, paraOperador(l.ID, l.SessaoID, l.Email, l.Assentos, l.TotalCentavos, l.Status, l.MotivoEstorno, l.CriadoEm))
	}
	return out, nil
}

// DetalharParaOperador devolve o pedido com ingressos e trilha.
func (s *Servico) DetalharParaOperador(ctx context.Context, id uuid.UUID) (DetalheOperador, error) {
	q := db.New(s.pool)
	linhas, err := q.PedidoOperador(ctx, id)
	if err != nil {
		return DetalheOperador{}, fmt.Errorf("pedido: detalhar para o operador: %w", err)
	}
	if len(linhas) == 0 {
		return DetalheOperador{}, ErrPedidoNaoEncontrado
	}
	l := linhas[0]
	d := DetalheOperador{PedidoOperador: paraOperador(l.ID, l.SessaoID, l.Email, l.Assentos, l.TotalCentavos, l.Status, l.MotivoEstorno, l.CriadoEm)}
	ingressos, err := q.IngressosDoPedido(ctx, id)
	if err != nil {
		return DetalheOperador{}, fmt.Errorf("pedido: ingressos para o operador: %w", err)
	}
	for _, i := range ingressos {
		d.Ingressos = append(d.Ingressos, IngressoOperador{Assento: i.AssentoCodigo, Status: i.Status})
	}
	eventos, err := q.EventosDoPedido(ctx, id)
	if err != nil {
		return DetalheOperador{}, fmt.Errorf("pedido: trilha para o operador: %w", err)
	}
	for _, e := range eventos {
		ev := EventoOperador{Para: e.Para, OcorridoEm: e.OcorridoEm}
		if e.De != nil {
			ev.De = *e.De
		}
		d.Eventos = append(d.Eventos, ev)
	}
	return d, nil
}

// CancelarPeloOperador cancela um pedido pago sem a janela do cliente, mas
// nunca depois do início da sessão (ADR 0011). A ação entra na trilha de
// auditoria na mesma TX. Pedido já em estorno → nada muda (idempotente).
func (s *Servico) CancelarPeloOperador(ctx context.Context, id uuid.UUID) (DetalheOperador, error) {
	agora := s.cfg.Agora()
	transicionou := false
	err := outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		q := db.New(tx)
		linhas, err := q.TravarParaCancelar(ctx, id)
		if err != nil {
			return fmt.Errorf("pedido: travar para cancelar: %w", err)
		}
		if len(linhas) == 0 {
			return ErrPedidoNaoEncontrado
		}
		jaCancelado, err := s.regraDoOperador(ctx, linhas[0], agora)
		if err != nil || jaCancelado {
			return err
		}
		if err := cancelarNaTx(ctx, q, id, MotivoOperador, agora); err != nil {
			return err
		}
		transicionou = true
		return auditoria.Registrar(ctx, tx, auditoria.PedidoCancelado, id.String(), agora)
	})
	if err != nil {
		return DetalheOperador{}, err
	}
	if transicionou {
		s.cfg.Cancelamento(ctx, OrigemOperador, 1)
		s.logger.Info("pedido: cancelado pelo operador", zap.String("pedido_id", id.String()),
			zap.String("operador_id", auditoria.AtorDe(ctx).String()))
	}
	return s.DetalharParaOperador(ctx, id)
}

// regraDoOperador: só pedido pago, sem ingresso usado, com a sessão ainda por
// começar; jaCancelado = já em estorno.
func (s *Servico) regraDoOperador(ctx context.Context, l db.TravarParaCancelarRow, agora time.Time) (jaCancelado bool, err error) {
	switch Status(l.Status) {
	case EstornoPendente, Estornado:
		return true, nil
	case Pago:
	case AguardandoPagamento, Expirado, Falhou:
		return false, ErrNaoCancelavel
	}
	if l.TemUsado {
		return false, ErrNaoCancelavel
	}
	inicio, _, ok, err := s.cfg.Sessoes.InicioDaSessao(ctx, l.SessaoID)
	if err != nil {
		return false, fmt.Errorf("pedido: sessão do cancelamento: %w", err)
	}
	if !ok || !agora.Before(inicio) {
		return false, ErrSessaoJaComecou
	}
	return false, nil
}
