package notificacao

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
)

func TestEfeito(t *testing.T) {
	agora := time.Date(2098, 1, 1, 12, 0, 3, 0, time.UTC)
	var latencias []time.Duration
	var entregues []uuid.UUID
	falhar := false
	c := NovoConsumidor(Config{
		Agora:    func() time.Time { return agora },
		Latencia: func(_ context.Context, d time.Duration) { latencias = append(latencias, d) },
		Entregar: func(_ context.Context, id uuid.UUID) error {
			if falhar {
				return errors.New("smtp fora")
			}
			entregues = append(entregues, id)
			return nil
		},
	}, zap.NewNop())
	id := uuid.New()
	msg := outbox.Mensagem{Payload: []byte(`{"pedido_id":"` + id.String() + `"}`), OccurredAt: agora.Add(-3 * time.Second)}

	if err := c.Efeito(context.Background(), nil, msg); err != nil || len(entregues) != 1 || entregues[0] != id ||
		len(latencias) != 1 || latencias[0] != 3*time.Second {
		t.Fatalf("efeito: %v entregues=%v latencias=%v", err, entregues, latencias)
	}

	// Falha na entrega é transitória (redelivery → DLQ), nunca permanente.
	falhar = true
	if err := c.Efeito(context.Background(), nil, msg); err == nil || errors.Is(err, outbox.ErrPermanente) {
		t.Fatalf("falha de entrega: %v", err)
	}

	// Payload inválido vai direto para a DLQ.
	for nome, corpo := range map[string]string{"ilegível": "{", "sem id": `{}`, "id inválido": `{"pedido_id":"x"}`} {
		if err := c.Efeito(context.Background(), nil, outbox.Mensagem{Payload: []byte(corpo)}); !errors.Is(err, outbox.ErrPermanente) {
			t.Errorf("%s: %v", nome, err)
		}
	}

	stub := NovoConsumidor(Config{}, zap.NewNop())
	if err := stub.Efeito(context.Background(), nil, msg); err != nil {
		t.Fatalf("stub: %v", err)
	}
}
