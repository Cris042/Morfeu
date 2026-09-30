package pedido

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/mclovin137/morfeu/internal/pedido/db"
)

// ErrSemWebhook: a rota de teste só funciona com o segredo do webhook.
var ErrSemWebhook = errors.New("pedido: pagamento de teste exige o webhook configurado")

// PagarParaTeste simula o cliente pagando (PRD 0031, decisão do usuário no
// refinamento E8): monta um payment_intent.succeeded, ASSINA com o segredo
// do webhook e o passa pelo mesmo Verificar + ProcessarEvento da rota real —
// o E2E exercita a assinatura, o pivô, a outbox e o e-mail. A rota que chama
// isto só é registrada com o gateway fake, que o boot recusa em produção.
func (s *Servico) PagarParaTeste(ctx context.Context, id uuid.UUID) error {
	if s.cfg.Webhook == nil {
		return ErrSemWebhook
	}
	linhas, err := db.New(s.pool).TravarPedido(ctx, id) // leitura simples (fora de TX)
	if err != nil {
		return fmt.Errorf("pedido: pagamento de teste: %w", err)
	}
	if len(linhas) == 0 || linhas[0].PaymentIntentID == nil {
		return ErrPedidoNaoEncontrado
	}
	p := linhas[0]
	corpo, assinatura, err := s.cfg.Webhook.EventoAprovadoDeTeste("evt_teste_"+uuid.NewString(), *p.PaymentIntentID, p.ID, p.TotalCentavos, Moeda)
	if err != nil {
		return err
	}
	ev, err := s.cfg.Webhook.Verificar(corpo, assinatura)
	if err != nil {
		return err
	}
	return s.ProcessarEvento(ctx, ev)
}
