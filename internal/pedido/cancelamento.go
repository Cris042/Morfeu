package pedido

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/pedido/db"
)

// Cancelamento como nova entrada da saga (ADR 0011, PRD 0036): o pedido pago
// vai a estorno_pendente por CAS, com os ingressos cancelados na mesma TX; os
// assentos só voltam à venda quando o job de estorno conclui (ADR 0010).

// JanelaCancelamento: o cliente cancela até 2h antes do início (doc.md §2).
const JanelaCancelamento = 2 * time.Hour

// Origens do cancelamento (cancelamentos_total{origem} — valores fixos).
const (
	OrigemCliente  = "cliente"
	OrigemOperador = "operador"
	OrigemSessao   = "sessao"
)

// DentroDaJanela diz se o cliente ainda pode cancelar: agora ≤ início − 2h
// (fronteira inclusiva — refinamento E9).
func DentroDaJanela(inicio, agora time.Time) bool {
	return !agora.After(inicio.Add(-JanelaCancelamento))
}

// marcarCancelaveis preenche Visao.Cancelavel (pago e dentro da janela),
// consultando cada sessão uma vez.
func (s *Servico) marcarCancelaveis(ctx context.Context, vs []Visao) error {
	agora := s.cfg.Agora()
	inicios := map[int64]time.Time{}
	for i := range vs {
		if vs[i].Status != Pago {
			continue
		}
		inicio, achou := inicios[vs[i].SessaoID]
		if !achou {
			ini, _, ok, err := s.cfg.Sessoes.InicioDaSessao(ctx, vs[i].SessaoID)
			if err != nil {
				return fmt.Errorf("pedido: sessão do pedido: %w", err)
			}
			if !ok {
				continue
			}
			inicio, inicios[vs[i].SessaoID] = ini, ini
		}
		vs[i].Cancelavel = DentroDaJanela(inicio, agora)
	}
	return nil
}

// CancelarDaConta cancela o pedido da conta logada (RF05). Pedido de outra
// conta ou inexistente → ErrPedidoNaoEncontrado (404, nunca 403).
func (s *Servico) CancelarDaConta(ctx context.Context, id, usuarioID uuid.UUID) (Visao, error) {
	dono := func(u *uuid.UUID) bool { return u != nil && *u == usuarioID }
	if err := s.cancelarPeloCliente(ctx, id, dono); err != nil {
		return Visao{}, err
	}
	return s.Obter(ctx, id, nil, &usuarioID)
}

// CancelarPorConsulta cancela o pedido do convidado por e-mail + código
// (RF06): mesma localização em tempo constante da consulta.
func (s *Servico) CancelarPorConsulta(ctx context.Context, email, codigo string) (ResultadoConsulta, error) {
	p, err := s.localizar(ctx, email, codigo)
	if err != nil {
		return ResultadoConsulta{}, err
	}
	if err := s.cancelarPeloCliente(ctx, p.ID, func(*uuid.UUID) bool { return true }); err != nil {
		return ResultadoConsulta{}, err
	}
	return s.Consultar(ctx, email, codigo)
}

// cancelarPeloCliente aplica as regras do cliente (RF04) numa TX com o
// pedido travado. Pedido já em estorno → nada muda (idempotente).
func (s *Servico) cancelarPeloCliente(ctx context.Context, id uuid.UUID, dono func(*uuid.UUID) bool) error {
	agora := s.cfg.Agora()
	transicionou := false
	err := outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		q := db.New(tx)
		linhas, err := q.TravarParaCancelar(ctx, id)
		if err != nil {
			return fmt.Errorf("pedido: travar para cancelar: %w", err)
		}
		if len(linhas) == 0 || !dono(linhas[0].UsuarioID) {
			return ErrPedidoNaoEncontrado
		}
		jaCancelado, err := s.regraDoCliente(ctx, linhas[0], agora)
		if err != nil || jaCancelado {
			return err
		}
		if err := cancelarNaTx(ctx, q, id, MotivoCancelamento, agora); err != nil {
			return err
		}
		transicionou = true
		return nil
	})
	if err != nil {
		return err
	}
	if transicionou {
		s.cfg.Cancelamento(ctx, OrigemCliente, 1)
		s.logger.Info("pedido: cancelado pelo cliente", zap.String("pedido_id", id.String()))
	}
	return nil
}

// regraDoCliente aplica RN01/RF03 ao pedido travado; jaCancelado = o pedido
// já está em estorno (o cancelamento repetido não muda nada).
func (s *Servico) regraDoCliente(ctx context.Context, l db.TravarParaCancelarRow, agora time.Time) (jaCancelado bool, err error) {
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
	if !ok || !DentroDaJanela(inicio, agora) {
		return false, ErrForaDaJanela
	}
	return false, nil
}

// CancelarPedidosDaSessao é a porta do módulo sessao (ADR 0011, RF11): na TX
// do cancelamento da sessão, trava TODOS os pedidos dela (um pivô em curso
// termina antes) e manda os pagos para estorno. Devolve quantos foram.
func (s *Servico) CancelarPedidosDaSessao(ctx context.Context, tx outbox.Tx, sessaoID int64) (int64, error) {
	q := db.New(tx)
	linhas, err := q.TravarPedidosDaSessao(ctx, sessaoID)
	if err != nil {
		return 0, fmt.Errorf("pedido: travar pedidos da sessão: %w", err)
	}
	agora := s.cfg.Agora()
	var n int64
	for _, l := range linhas {
		if Status(l.Status) != Pago {
			continue
		}
		if err := cancelarNaTx(ctx, q, l.ID, MotivoSessaoCancelada, agora); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

// ContarCancelamentosDaSessao registra na métrica os pedidos que a porta
// cancelou — chamado pelo main DEPOIS do commit da TX da sessão.
func (s *Servico) ContarCancelamentosDaSessao(ctx context.Context, n int64) {
	if n > 0 {
		s.cfg.Cancelamento(ctx, OrigemSessao, n)
	}
}

// cancelarNaTx: CAS pago → estorno_pendente com o motivo + trilha + ingressos
// cancelados, tudo na TX recebida.
func cancelarNaTx(ctx context.Context, q *db.Queries, id uuid.UUID, motivo string, agora time.Time) error {
	if err := (repositorio{q: q}).marcarEstornoPor(ctx, id, Pago, CancelamentoSolicitado, motivo, agora); err != nil {
		return err
	}
	if _, err := q.CancelarIngressosDoPedido(ctx, id); err != nil {
		return fmt.Errorf("pedido: cancelar ingressos: %w", err)
	}
	return nil
}
