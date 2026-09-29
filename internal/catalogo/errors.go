package catalogo

import "errors"

// Erros de domínio do catálogo, mapeados para HTTP no handler.
var (
	// ErrDadosInvalidos: entrada fora das regras (campos em ErroValidacao).
	ErrDadosInvalidos = errors.New("catalogo: dados inválidos")
	// ErrFilmeNaoEncontrado: id inexistente (ou arquivado, na leitura pública).
	ErrFilmeNaoEncontrado = errors.New("catalogo: filme não encontrado")
)

// ErroValidacao lista os campos inválidos (sem ecoar os valores).
type ErroValidacao struct {
	Campos []string
}

func (e *ErroValidacao) Error() string { return ErrDadosInvalidos.Error() }

// Unwrap permite errors.Is(err, ErrDadosInvalidos).
func (e *ErroValidacao) Unwrap() error { return ErrDadosInvalidos }
