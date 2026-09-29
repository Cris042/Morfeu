package db

// Arquivo NÃO gerado pelo sqlc (PRD 0013 RF08): único ponto do módulo sessao
// que conhece o tipo de erro do driver. O domínio pergunta por significado
// (EhConflitoDeHorario/EhNomeDuplicado) e nunca importa pgconn (ADR 0003).

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	codigoViolacaoExclusao = "23P01"
	codigoViolacaoUnica    = "23505"

	constraintSemConflito = "sessoes_sem_conflito"
	constraintNomeSala    = "salas_nome_key"
)

// EhConflitoDeHorario informa se err é a violação da EXCLUDE que impede
// sessões agendadas sobrepostas na mesma sala.
func EhConflitoDeHorario(err error) bool {
	return violou(err, codigoViolacaoExclusao, constraintSemConflito)
}

// EhNomeDuplicado informa se err é a violação do nome único de sala.
func EhNomeDuplicado(err error) bool {
	return violou(err, codigoViolacaoUnica, constraintNomeSala)
}

func violou(err error, codigo, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == codigo && pgErr.ConstraintName == constraint
}
