package db

// Arquivo NÃO gerado pelo sqlc (PRD 0015 RNF01): único ponto do módulo
// reserva que conhece o tipo de erro do driver (ADR 0003).

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const codigoImpasse = "40P01"

// EhImpasse informa se err é deadlock_detected (40P01). A ordem global dos
// upserts evita a espera circular (ADR 0008); a repetição é defesa extra,
// aprendida com a EXCLUDE do E3 no CI ARM64.
func EhImpasse(err error) bool {
	return CodigoSQL(err) == codigoImpasse
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
