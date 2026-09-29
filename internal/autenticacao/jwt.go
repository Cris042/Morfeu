// Package autenticacao é a plataforma de autenticação/autorização (PRD 0008,
// refinamento E1): emite e valida o access token JWT, expõe o middleware de
// exigência de papel e o limitador de tentativas. Qualquer módulo autoriza
// por aqui sem importar internal/identidade (ADR 0003) — identidade é a dona
// dos usuários; este pacote só conhece claims.
package autenticacao

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TTLAccessPadrao é a validade do access token (decisão do usuário no
// refinamento E1: 10 min — curto porque o token não é revogável).
const TTLAccessPadrao = 10 * time.Minute

// tamanhoMinimoSegredo: HS256 exige chave com pelo menos 256 bits.
const tamanhoMinimoSegredo = 32

// algoritmo é o ÚNICO aceito na validação (allowlist — impede confusão de
// algoritmo e alg:none).
var algoritmo = jwt.SigningMethodHS256

// Papel é o papel do usuário (doc.md §5). Autorização decide só por ele.
type Papel string

// Papéis válidos (RN01).
const (
	PapelCliente  Papel = "cliente"
	PapelOperador Papel = "operador"
)

// Valido informa se p é um papel conhecido.
func (p Papel) Valido() bool {
	return p == PapelCliente || p == PapelOperador
}

// Erros do pacote. Causas detalhadas ficam na cadeia (errors.Is) e nunca vão
// para a resposta HTTP.
var (
	ErrConfigInvalida = errors.New("autenticacao: configuração inválida")
	ErrTokenInvalido  = errors.New("autenticacao: token inválido")
)

// Claims do access token: só sub (id do usuário), papel, iat e exp — nenhuma
// PII (JWT é assinado, não cifrado).
type Claims struct {
	Papel Papel `json:"papel"`
	jwt.RegisteredClaims
}

// ConfigJWT parametriza o Emissor. Agora é injetável para testes.
type ConfigJWT struct {
	Segredo []byte
	Kid     string
	TTL     time.Duration
	Agora   func() time.Time
}

// Emissor emite e valida access tokens com uma única chave identificada por
// Kid. Rotação automática fica para a Fase 2; o kid já permite invalidação em
// massa trocando segredo+kid.
type Emissor struct {
	segredo []byte
	kid     string
	ttl     time.Duration
	agora   func() time.Time
}

// NovoEmissor valida a configuração (segredo ≥ 32 bytes, kid, TTL > 0).
func NovoEmissor(cfg ConfigJWT) (*Emissor, error) {
	switch {
	case len(cfg.Segredo) < tamanhoMinimoSegredo:
		return nil, fmt.Errorf("%w: segredo JWT precisa de pelo menos %d bytes", ErrConfigInvalida, tamanhoMinimoSegredo)
	case cfg.Kid == "":
		return nil, fmt.Errorf("%w: kid vazio", ErrConfigInvalida)
	case cfg.TTL <= 0:
		return nil, fmt.Errorf("%w: TTL precisa ser positivo", ErrConfigInvalida)
	}
	agora := cfg.Agora
	if agora == nil {
		agora = time.Now
	}
	segredo := make([]byte, len(cfg.Segredo))
	copy(segredo, cfg.Segredo)
	return &Emissor{segredo: segredo, kid: cfg.Kid, ttl: cfg.TTL, agora: agora}, nil
}

// Emitir cria o access token do usuário com o papel informado.
func (e *Emissor) Emitir(usuarioID uuid.UUID, papel Papel) (string, time.Time, error) {
	if usuarioID == uuid.Nil || !papel.Valido() {
		return "", time.Time{}, fmt.Errorf("autenticacao: usuário ou papel inválido para emissão")
	}
	agora := e.agora().UTC().Truncate(time.Second)
	expira := agora.Add(e.ttl)

	token := jwt.NewWithClaims(algoritmo, Claims{
		Papel: papel,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   usuarioID.String(),
			IssuedAt:  jwt.NewNumericDate(agora),
			ExpiresAt: jwt.NewNumericDate(expira),
		},
	})
	token.Header["kid"] = e.kid

	assinado, err := token.SignedString(e.segredo)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("autenticacao: assinar token: %w", err)
	}
	return assinado, expira, nil
}

// Validar verifica assinatura, algoritmo, kid, exp/iat e o conteúdo das
// claims. Qualquer falha devolve ErrTokenInvalido (causa encadeada).
func (e *Emissor) Validar(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, e.chaveDoToken,
		jwt.WithValidMethods([]string{algoritmo.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(e.agora),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTokenInvalido, err)
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, fmt.Errorf("%w: sub não é um UUID", ErrTokenInvalido)
	}
	if !claims.Papel.Valido() {
		return nil, fmt.Errorf("%w: papel desconhecido", ErrTokenInvalido)
	}
	return claims, nil
}

// chaveDoToken só devolve a chave quando o kid do header é exatamente o
// configurado — token de outra chave (ou sem kid) nunca é verificado.
func (e *Emissor) chaveDoToken(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	if kid != e.kid {
		return nil, fmt.Errorf("kid %q desconhecido", kid)
	}
	return e.segredo, nil
}

// UsuarioIDDe devolve o id do usuário das claims (sub já validado).
func (c *Claims) UsuarioIDDe() uuid.UUID {
	id, _ := uuid.Parse(c.Subject)
	return id
}
