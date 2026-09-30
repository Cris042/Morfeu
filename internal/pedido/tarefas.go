package pedido

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/pedido/db"
	"github.com/mclovin137/morfeu/internal/pedido/pagamento"
)

// Tarefas periódicas do worker (PRD 0025, ADR 0010): a saga não tem fila —
// o que falha fora da TX é recuperado aqui, sempre pelo mesmo CAS.
const (
	IntervaloTarefas     = time.Minute
	loteReconciliacao    = 20 // teto por rodada (Stripe test ~25 req/s)
	loteEstornos         = 10
	backoffEstornoBase   = time.Minute
	backoffEstornoMaximo = time.Hour
	// alertaTentativas: a partir daqui cada falha é log de erro (o alerta
	// versionado vem na task 0026).
	alertaTentativas = 5
)

// ResumoTarefas conta o que uma rodada fez (logs e testes).
type ResumoTarefas struct {
	Expirados   int
	Confirmados int
	Estornados  int
	Falhas      int
}

// RodarTarefas executa reconciliação e estornos a cada intervalo até ctx acabar.
func (s *Servico) RodarTarefas(ctx context.Context, intervalo time.Duration) {
	ticker := time.NewTicker(intervalo)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r := s.Reconciliar(ctx)
			e := s.ExecutarEstornos(ctx)
			if total := r.Expirados + r.Confirmados + e.Estornados + r.Falhas + e.Falhas; total > 0 {
				s.logger.Info("pedido: tarefas da saga",
					zap.Int("expirados", r.Expirados), zap.Int("confirmados", r.Confirmados),
					zap.Int("estornados", e.Estornados), zap.Int("falhas", r.Falhas+e.Falhas))
			}
		}
	}
}

// Reconciliar trata os pedidos pendentes vencidos (RF01–RF03): cobrança
// aprovada sem webhook → o mesmo pivô do webhook; senão cancela a cobrança e
// expira o pedido, com os assentos de volta. Cobrança que o gateway não deixa
// cancelar (acabou de ser paga) fica para a próxima rodada.
func (s *Servico) Reconciliar(ctx context.Context) ResumoTarefas {
	var out ResumoTarefas
	vencidos, err := db.New(s.pool).PedidosVencidos(ctx, db.PedidosVencidosParams{Agora: s.cfg.Agora(), Limite: loteReconciliacao})
	if err != nil {
		s.logger.Error("pedido: listar vencidos", zap.Error(err))
		out.Falhas++
		return out
	}
	for _, v := range vencidos {
		confirmou, err := s.reconciliarUm(ctx, v.ID, v.PaymentIntentID)
		switch {
		case err != nil:
			out.Falhas++
			s.logger.Warn("pedido: reconciliação adiada", zap.String("pedido_id", v.ID.String()), zap.Error(err))
		case confirmou:
			out.Confirmados++
		default:
			out.Expirados++
		}
	}
	return out
}

func (s *Servico) reconciliarUm(ctx context.Context, id uuid.UUID, intencao *string) (bool, error) {
	if intencao == nil {
		return false, s.expirarAgora(ctx, id)
	}
	sit, err := s.cfg.Gateway.ConsultarCobranca(ctx, *intencao)
	if err != nil {
		return false, fmt.Errorf("consultar cobrança: %w", err)
	}
	switch sit.Estado {
	case pagamento.CobrancaAprovada:
		// Webhook perdido: aplica o mesmo pivô (idempotente pelo CAS).
		return true, s.AplicarPagamento(ctx, Pagamento{PedidoID: id, IntencaoID: *intencao, ValorCentavos: sit.ValorCentavos, Moeda: sit.Moeda})
	case pagamento.CobrancaPendente:
		if err := s.cfg.Gateway.CancelarCobranca(ctx, *intencao); err != nil {
			return false, fmt.Errorf("cancelar cobrança: %w", err)
		}
	case pagamento.CobrancaCancelada:
	}
	return false, s.expirarAgora(ctx, id)
}

// expirarAgora expira o pedido numa TX própria.
func (s *Servico) expirarAgora(ctx context.Context, id uuid.UUID) error {
	return outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		_, err := s.expirar(ctx, tx, repositorio{q: db.New(tx)}, id, s.cfg.Agora())
		return err
	})
}

// ExecutarEstornos devolve o dinheiro dos pedidos em estorno_pendente
// (RF04–RF06): chamada ao gateway FORA de TX, com a chave estorno-{pedido}
// (repetir nunca estorna duas vezes); sucesso → estornado + assentos
// devolvidos; falha → tentativas++ e backoff exponencial (1 min … 1 h).
func (s *Servico) ExecutarEstornos(ctx context.Context) ResumoTarefas {
	var out ResumoTarefas
	agora := s.cfg.Agora()
	pendentes, err := db.New(s.pool).EstornosPendentes(ctx, loteEstornos)
	if err != nil {
		s.logger.Error("pedido: listar estornos pendentes", zap.Error(err))
		out.Falhas++
		return out
	}
	for _, p := range pendentes {
		if p.TentativasEstorno > 0 && agora.Before(p.AtualizadoEm.Add(backoffEstorno(int(p.TentativasEstorno)))) {
			continue
		}
		if err := s.estornarUm(ctx, p.ID, p.PaymentIntentID, int(p.TentativasEstorno)); err != nil {
			out.Falhas++
			continue
		}
		out.Estornados++
	}
	return out
}

// ChaveEstorno é a chave de idempotência do estorno de um pedido.
func ChaveEstorno(id uuid.UUID) string { return "estorno-" + id.String() }

func (s *Servico) estornarUm(ctx context.Context, id uuid.UUID, intencao *string, tentativas int) error {
	campos := []zap.Field{zap.String("pedido_id", id.String()), zap.Int("tentativas", tentativas)}
	if intencao == nil {
		// Não deveria existir (o pivô sempre grava a cobrança): exige olhar humano.
		s.logger.Error("pedido: estorno pendente sem cobrança", campos...)
		return errors.New("pedido: estorno sem cobrança")
	}
	if err := s.cfg.Gateway.Estornar(ctx, *intencao, ChaveEstorno(id)); err != nil {
		nivel := s.logger.Warn
		if tentativas+1 >= alertaTentativas {
			nivel = s.logger.Error
		}
		nivel("pedido: estorno falhou", append(campos, zap.Error(err))...)
		if _, errR := db.New(s.pool).RegistrarFalhaEstorno(ctx, db.RegistrarFalhaEstornoParams{Agora: s.cfg.Agora(), ID: id}); errR != nil {
			s.logger.Error("pedido: registrar falha do estorno", append(campos, zap.Error(errR))...)
		}
		return err
	}
	err := outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		r := repositorio{q: db.New(tx)}
		if _, err := r.transicionar(ctx, id, EstornoPendente, EstornoConcluido, s.cfg.Agora()); err != nil {
			return err
		}
		// Divergência com o pedido ainda aguardando deixou holds presos.
		_, err := s.cfg.Reserva.LiberarDoPedido(ctx, tx, id)
		return err
	})
	if errors.Is(err, ErrTransicaoConcorrente) {
		return nil // outra instância concluiu o mesmo estorno (mesma chave)
	}
	if err != nil {
		// O dinheiro já voltou; na próxima rodada a mesma chave confirma e o CAS fecha.
		s.logger.Error("pedido: estorno feito mas não registrado", append(campos, zap.Error(err))...)
		return err
	}
	s.cfg.Funil(ctx, EtapaEstornado)
	s.logger.Info("pedido: estornado", campos...)
	return nil
}

// backoffEstorno: 1, 2, 4, … minutos por tentativa, no máximo 1 h.
func backoffEstorno(tentativas int) time.Duration {
	d := backoffEstornoBase
	for i := 1; i < tentativas && d < backoffEstornoMaximo; i++ {
		d *= 2
	}
	return min(d, backoffEstornoMaximo)
}
