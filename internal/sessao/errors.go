package sessao

import (
	"errors"
	"time"
)

// Erros de domínio do módulo sessao, mapeados para HTTP no handler.
var (
	ErrDadosInvalidos      = errors.New("sessao: dados inválidos")
	ErrSalaNaoEncontrada   = errors.New("sessao: sala não encontrada")
	ErrSalaInexistente     = errors.New("sessao: sala inexistente") // referenciada na criação de sessão (422)
	ErrNomeSalaEmUso       = errors.New("sessao: nome de sala em uso")
	ErrLayoutEmUso         = errors.New("sessao: layout com sessão agendada futura")
	ErrFilmeIndisponivel   = errors.New("sessao: filme inexistente, arquivado ou sem duração")
	ErrSessaoNaoEncontrada = errors.New("sessao: sessão não encontrada")
)

// ErroValidacao lista os campos inválidos (sem ecoar os valores).
type ErroValidacao struct {
	Campos []string
}

func (e *ErroValidacao) Error() string { return ErrDadosInvalidos.Error() }

// Unwrap permite errors.Is(err, ErrDadosInvalidos).
func (e *ErroValidacao) Unwrap() error { return ErrDadosInvalidos }

// ErroConflitoHorario carrega o horário da sessão que ocupa a sala (RF04 —
// o 409 informa o conflito ao operador).
type ErroConflitoHorario struct {
	SessaoID int64
	Inicio   time.Time
	Fim      time.Time
}

func (e *ErroConflitoHorario) Error() string {
	return "sessao: conflito de horário com a sessão das " + e.Inicio.UTC().Format(time.RFC3339)
}
