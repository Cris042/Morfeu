// Package auditoria é a trilha enxuta das ações do operador (doc.md §10,
// refinamento E9, PRD 0037): só IDs — ator, ação de um enum fechado, alvo e
// instante —, gravada na MESMA TX da ação (mutação sem trilha não existe) e
// purgada depois de 12 meses. Ownership de plataforma, como o outbox: a
// tabela eventos_auditoria não pertence a nenhum domínio; cada service grava
// nela pela função Registrar, com a TX que já abriu.
//
// O ator viaja no context da requisição (ComAtor, ligado no main junto do
// middleware do operador): os services não mudam de assinatura e nenhum
// domínio importa autenticação (ADR 0003).
package auditoria

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/mclovin137/morfeu/internal/outbox"
)

// Acao é o que o operador fez (CHECK da migration 017 — enum fechado).
type Acao string

// Ações auditadas.
const (
	FilmeCriado     Acao = "filme_criado"
	FilmeAtualizado Acao = "filme_atualizado"
	FilmeArquivado  Acao = "filme_arquivado"
	FilmeImportado  Acao = "filme_importado"
	SalaCriada      Acao = "sala_criada"
	SalaAtualizada  Acao = "sala_atualizada"
	SessaoCriada    Acao = "sessao_criada"
	SessaoCancelada Acao = "sessao_cancelada"
	PedidoCancelado Acao = "pedido_cancelado"
)

// alvos: cada ação diz o tipo do alvo — o chamador só informa o id.
var alvos = map[Acao]string{
	FilmeCriado: "filme", FilmeAtualizado: "filme", FilmeArquivado: "filme", FilmeImportado: "filme",
	SalaCriada: "sala", SalaAtualizada: "sala",
	SessaoCriada: "sessao", SessaoCancelada: "sessao",
	PedidoCancelado: "pedido",
}

// Retencao da trilha (doc.md §10).
const Retencao = 365 * 24 * time.Hour

type chaveAtor struct{}

// ComAtor devolve o context com o operador que age na requisição.
func ComAtor(ctx context.Context, ator uuid.UUID) context.Context {
	return context.WithValue(ctx, chaveAtor{}, ator)
}

// AtorDe lê o operador do context; sem ator (seed, jobs, testes de módulo) é
// uuid.Nil — o "sistema".
func AtorDe(ctx context.Context) uuid.UUID {
	if a, ok := ctx.Value(chaveAtor{}).(uuid.UUID); ok {
		return a
	}
	return uuid.Nil
}

// Registrar grava a ação na TX do chamador; erro = a ação inteira volta
// (rollback). O alvo é numérico (filme, sala, sessão) ou UUID (pedido).
func Registrar(ctx context.Context, tx outbox.Tx, acao Acao, alvoID string, agora time.Time) error {
	tipo, ok := alvos[acao]
	if !ok {
		return fmt.Errorf("auditoria: ação desconhecida %q", acao)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO eventos_auditoria (ator_id, acao, alvo_tipo, alvo_id, ocorrido_em) VALUES ($1, $2, $3, $4, $5)`,
		AtorDe(ctx), string(acao), tipo, alvoID, agora); err != nil {
		return fmt.Errorf("auditoria: registrar %s: %w", acao, err)
	}
	return nil
}

// ID formata um id numérico de alvo.
func ID(id int64) string { return strconv.FormatInt(id, 10) }

// Purgar apaga, em lotes, os eventos anteriores a antesDe (retenção de 12
// meses) e devolve quantos saíram. Lotes curtos: nunca uma TX longa.
func Purgar(ctx context.Context, pool outbox.Pool, antesDe time.Time, lote int) (int64, error) {
	var total int64
	for {
		tag, err := pool.Exec(ctx,
			`DELETE FROM eventos_auditoria WHERE id IN (SELECT id FROM eventos_auditoria WHERE ocorrido_em < $1 ORDER BY id LIMIT $2)`,
			antesDe, lote)
		if err != nil {
			return total, fmt.Errorf("auditoria: purgar: %w", err)
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < int64(lote) || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

// LotePurga limita cada DELETE da purga.
const LotePurga = 5000

// RodarPurga purga na partida e depois a cada intervalo (worker), até o ctx
// acabar. aoPurgar recebe o total removido em cada rodada (métricas).
func RodarPurga(ctx context.Context, pool outbox.Pool, intervalo time.Duration, agora func() time.Time, aoPurgar func(n int64, err error)) {
	t := time.NewTicker(intervalo)
	defer t.Stop()
	for {
		n, err := Purgar(ctx, pool, agora().Add(-Retencao), LotePurga)
		if ctx.Err() != nil {
			return
		}
		aoPurgar(n, err)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
