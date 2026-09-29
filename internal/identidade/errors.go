package identidade

import "errors"

// Erros de domínio do módulo identidade, mapeados para HTTP no handler.
var (
	// ErrDadosInvalidos: entrada fora das regras (campos em ErroValidacao).
	ErrDadosInvalidos = errors.New("identidade: dados inválidos")
	// ErrEmailEmUso: e-mail já cadastrado (registro).
	ErrEmailEmUso = errors.New("identidade: e-mail em uso")
	// ErrCredenciaisInvalidas: falha de login — MESMO erro para conta
	// inexistente e senha errada (RN03, anti-enumeração).
	ErrCredenciaisInvalidas = errors.New("identidade: credenciais inválidas")
	// ErrMuitasTentativas: limitador por conta ou IP atingido.
	ErrMuitasTentativas = errors.New("identidade: muitas tentativas")
	// ErrSaturado: semáforo de hashing cheio (proteção de CPU).
	ErrSaturado = errors.New("identidade: serviço de autenticação saturado")
	// ErrUsuarioNaoEncontrado: o id do token não existe mais no banco.
	ErrUsuarioNaoEncontrado = errors.New("identidade: usuário não encontrado")
)

// ErroValidacao lista os campos inválidos (sem ecoar os valores).
type ErroValidacao struct {
	Campos []string
}

func (e *ErroValidacao) Error() string { return ErrDadosInvalidos.Error() }

// Unwrap permite errors.Is(err, ErrDadosInvalidos).
func (e *ErroValidacao) Unwrap() error { return ErrDadosInvalidos }
