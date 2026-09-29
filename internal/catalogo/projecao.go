package catalogo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mclovin137/morfeu/internal/catalogo/db"
	"github.com/mclovin137/morfeu/internal/outbox"
)

// ConsumidorProjecaoFilmes identifica este consumidor em processed_messages:
// o dedup é por (message_id, consumidor), então outro consumidor do mesmo
// evento (ex.: e-mail, E6) tem o próprio registro de dedup.
const ConsumidorProjecaoFilmes = "catalogo.projecao_filmes"

// ProjetarFilmeCriado é o efeito de domínio do evento catalogo.filme_criado
// (RF07 do PRD 0005): projeta o filme em catalogo_filmes_projetados usando a
// tx do wrapper de dedup (outbox.ProcessarUmaVez) — nunca abre transação.
// Payload ilegível ou sem id/título é permanente: vai à DLQ sem retry.
func ProjetarFilmeCriado(ctx context.Context, tx outbox.Tx, msg outbox.Mensagem) error {
	if msg.EventType != eventoFilmeCriado {
		return fmt.Errorf("event_type %q inesperado: %w", msg.EventType, outbox.ErrPermanente)
	}

	var p filmeCriadoPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return fmt.Errorf("payload de %s ilegível: %w: %w", eventoFilmeCriado, err, outbox.ErrPermanente)
	}
	if p.ID <= 0 || p.Titulo == "" {
		return fmt.Errorf("payload de %s sem id/titulo: %w", eventoFilmeCriado, outbox.ErrPermanente)
	}

	if err := db.New(tx).UpsertFilmeProjetado(ctx, db.UpsertFilmeProjetadoParams{
		FilmID: p.ID,
		Titulo: p.Titulo,
		Ano:    p.Ano,
	}); err != nil {
		return fmt.Errorf("projetar filme %d: %w", p.ID, err)
	}
	return nil
}
