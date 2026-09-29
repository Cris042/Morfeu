package outbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/broker"
	"github.com/mclovin137/morfeu/internal/outbox/db"
)

// ErrPermanente é o mesmo sentinela de broker.ErrPermanente, exposto aqui para
// que domínios marquem erros não-retentáveis (payload inválido) sem importar
// internal/broker (RF06 do PRD 0005). Embrulhe com %w: a entrega vai à DLQ.
var ErrPermanente = broker.ErrPermanente

// Mensagem é o evento recebido do broker, já validado, entregue ao efeito de
// domínio (RF05). Payload é o JSON publicado pelo produtor (RF05 da 0002).
type Mensagem struct {
	MessageID   uuid.UUID
	EventType   string
	AggregateID string
	Payload     []byte
}

// Efeito aplica a mensagem ao domínio usando exclusivamente tx — a mesma
// transação em que o dedup é registrado.
type Efeito func(ctx context.Context, tx Tx, msg Mensagem) error

// ProcessarUmaVez registra msg em processed_messages e aplica efeito NA MESMA
// TX (RF05, ADR 0007): commit de ambos ou de nenhum. Se o message_id já foi
// processado por este consumidor, o efeito não roda e duplicada=true (a
// entrega deve ser ack'ada). Erro do efeito desfaz também o registro de dedup,
// permitindo que a redelivery reaplique (CA02).
func ProcessarUmaVez(ctx context.Context, pool Pool, consumidor string, msg Mensagem, efeito Efeito) (duplicada bool, err error) {
	err = WithTx(ctx, pool, func(tx Tx) error {
		n, err := db.New(tx).RegistrarProcessada(ctx, db.RegistrarProcessadaParams{
			MessageID:  pgtype.UUID{Bytes: msg.MessageID, Valid: true},
			Consumidor: consumidor,
		})
		if err != nil {
			return fmt.Errorf("outbox: registrar mensagem processada: %w", err)
		}
		if n == 0 {
			duplicada = true
			return nil
		}
		return efeito(ctx, tx, msg)
	})
	if err != nil {
		return false, err
	}
	return duplicada, nil
}

// NovoHandler adapta um Efeito de domínio ao broker.Handler: valida o
// envelope, deduplica via ProcessarUmaVez e loga só metadados (RNF03 — nunca
// o payload). Sem message_id válido não há chave de dedup: ErrPermanente.
func NovoHandler(pool Pool, consumidor string, efeito Efeito, logger *zap.Logger) broker.Handler {
	return func(ctx context.Context, e broker.Entrega) error {
		id, err := uuid.Parse(e.MessageID)
		if err != nil {
			return fmt.Errorf("message_id %q inválido: %w", e.MessageID, ErrPermanente)
		}

		msg := Mensagem{MessageID: id, EventType: e.Type, AggregateID: e.AggregateID, Payload: e.Body}
		duplicada, err := ProcessarUmaVez(ctx, pool, consumidor, msg, efeito)
		if err != nil {
			return err
		}
		logger.Info("mensagem consumida",
			zap.String("consumidor", consumidor),
			zap.String("event_type", e.Type),
			zap.String("aggregate_id", e.AggregateID),
			zap.String("message_id", e.MessageID),
			zap.Bool("duplicada", duplicada),
		)
		return nil
	}
}
