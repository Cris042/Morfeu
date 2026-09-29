package reserva

import "errors"

// Erros de domínio do módulo reserva, mapeados para HTTP no handler.
var (
	ErrDadosInvalidos      = errors.New("reserva: dados inválidos")
	ErrSessaoIndisponivel  = errors.New("reserva: sessão inexistente, cancelada ou iniciada")
	ErrHoldNaoEncontrado   = errors.New("reserva: hold inexistente, vencido ou de outro dono")
	ErrExtensaoEsgotada    = errors.New("reserva: hold já estendido")
	ErrLimiteHolds         = errors.New("reserva: limite de holds vivos por dono")
	ErrMuitasRequisicoes   = errors.New("reserva: muitas requisições")
	ErrAssentoIndisponivel = errors.New("reserva: assento indisponível")
	errTravaSweeperOcupada = errors.New("reserva: sweeper já em execução")
)

// ErroValidacao lista os campos inválidos (sem ecoar os valores).
type ErroValidacao struct {
	Campos []string
}

func (e *ErroValidacao) Error() string { return ErrDadosInvalidos.Error() }

// Unwrap permite errors.Is(err, ErrDadosInvalidos).
func (e *ErroValidacao) Unwrap() error { return ErrDadosInvalidos }

// ErroIndisponivel lista os assentos do lote que outro dono já trava (RF04:
// o lote inteiro é desfeito e o 409 diz quais foram recusados).
type ErroIndisponivel struct {
	Assentos []string
}

func (e *ErroIndisponivel) Error() string { return ErrAssentoIndisponivel.Error() }

// Unwrap permite errors.Is(err, ErrAssentoIndisponivel).
func (e *ErroIndisponivel) Unwrap() error { return ErrAssentoIndisponivel }
