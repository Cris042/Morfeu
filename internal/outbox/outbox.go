// Package outbox implementa o lado produtor do padrão transactional outbox
// (PRD 0002, ADR 0007). Ownership excepcional da plataforma (RN03): a tabela
// outbox_events não pertence a nenhum domínio — qualquer service (ex.:
// catalogo) grava nela dentro da própria transação via Enqueue.
//
// Pool e Tx são realiases dos tipos do driver pgx: este pacote (junto com
// internal/broker) é a única fronteira que enxerga o driver diretamente
// (ADR 0003); domínios como catalogo dependem só destes aliases — nunca
// importam "github.com/jackc/pgx/v5" ou pgxpool por conta própria (depguard,
// .golangci.yml, regra catalogo-domain).
package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mclovin137/morfeu/internal/outbox/db"
)

// Pool é o alias de *pgxpool.Pool usado pelos domínios para abrir transações
// via WithTx, sem importar o driver diretamente.
type Pool = *pgxpool.Pool

// Tx é o alias de pgx.Tx usado pelos domínios para participar da mesma
// transação de Enqueue (RF01), sem importar o driver diretamente.
type Tx = pgx.Tx

// Evento é o evento de domínio a ser gravado na outbox (RF01). Payload já
// deve estar serializado como JSON — o pacote outbox não conhece o schema de
// cada evento de domínio (isso pertence ao módulo que o emite). OccurredAt é
// time.Time (não pgtype) de propósito: domínios como catalogo não importam
// pgtype diretamente (depguard, ADR 0003) — a conversão fica só aqui.
type Evento struct {
	EventType   string
	AggregateID string
	OccurredAt  time.Time
	Payload     []byte
}

// WithTx abre uma transação no pool informado, executa fn e faz commit se fn
// não retornar erro; rollback em qualquer erro (incluindo panic — a defer de
// Rollback é sempre chamada e é no-op após um Commit bem-sucedido). É o
// helper único de transação mencionado no ADR 0002 ("plataforma.WithTx"):
// o service do domínio é o único dono da transação (ADR 0003), mas abre-a
// através deste ponto único para nunca importar pgx/pgxpool diretamente.
func WithTx(ctx context.Context, pool Pool, fn func(tx Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("outbox: iniciar transação: %w", err)
	}
	defer func() {
		// Erro de rollback após commit bem-sucedido é esperado (tx já
		// finalizada) — pgx retorna pgx.ErrTxClosed, que é seguro ignorar aqui.
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("outbox: commit da transação: %w", err)
	}
	return nil
}

// Enqueue grava evt na tabela outbox_events usando a tx recebida — nunca abre
// transação própria (RF01). RN01: se a TX do chamador sofrer rollback, o
// evento nunca persiste junto com o efeito de domínio.
func Enqueue(ctx context.Context, tx Tx, evt Evento) (uuid.UUID, error) {
	q := db.New(tx)

	row, err := q.InsertEvent(ctx, db.InsertEventParams{
		EventType:   evt.EventType,
		AggregateID: evt.AggregateID,
		OccurredAt:  pgtype.Timestamptz{Time: evt.OccurredAt.UTC(), Valid: true},
		Payload:     evt.Payload,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: enfileirar evento %s: %w", evt.EventType, err)
	}

	id, err := uuid.FromBytes(row.ID.Bytes[:])
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: id do evento gravado é inválido: %w", err)
	}
	return id, nil
}

// Pendentes conta os eventos ainda não publicados (published_at IS NULL) —
// RF08, fonte do dado para o contador outbox_pendentes (E0d consome esta
// função; nenhuma métrica/endpoint entra nesta task).
func Pendentes(ctx context.Context, pool Pool) (int64, error) {
	q := db.New(pool)
	count, err := q.ContarPendentes(ctx)
	if err != nil {
		return 0, fmt.Errorf("outbox: contar pendentes: %w", err)
	}
	return count, nil
}

// LagSegundos devolve a idade, em segundos, do evento pendente mais antigo
// (0 se não há pendentes) — fonte do gauge morfeu_outbox_lag_segundos
// (RF04 do PRD 0006): mede quanto o relay está atrasado.
func LagSegundos(ctx context.Context, pool Pool) (float64, error) {
	idade, err := db.New(pool).IdadePendenteMaisAntigo(ctx)
	if err != nil {
		return 0, fmt.Errorf("outbox: idade do pendente mais antigo: %w", err)
	}
	return idade, nil
}
