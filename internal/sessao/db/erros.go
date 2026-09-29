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
	codigoImpasse          = "40P01"

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

// EhImpasse informa se err é deadlock_detected (40P01). Com a EXCLUDE, dois
// INSERTs concorrentes conflitantes podem esperar um pelo outro e o PG aborta
// um deles com deadlock em vez de 23P01 (visto no CI ARM64, PR #38) — a
// tentativa pode ser repetida: a concorrente já terá commitado e o novo
// INSERT recebe o 23P01 (409) ou passa.
func EhImpasse(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == codigoImpasse
}

// CodigoSQL devolve o SQLSTATE de err (vazio se não for erro do PG) — só p/
// diagnóstico em log.
func CodigoSQL(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func violou(err error, codigo, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == codigo && pgErr.ConstraintName == constraint
}
