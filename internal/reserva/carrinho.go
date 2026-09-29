package reserva

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

// tamanhoToken: 32 bytes de crypto/rand = 256 bits (refinamento E4, security).
const tamanhoToken = 32

// Dono é a identidade do carrinho que detém os holds: só o SHA-256 do token
// (o valor em claro vive apenas no cookie do navegador — RF03).
type Dono struct {
	hash []byte
}

// NovoCarrinho gera um token opaco e o dono que ele identifica.
func NovoCarrinho() (token string, d Dono, err error) {
	b := make([]byte, tamanhoToken)
	if _, err := rand.Read(b); err != nil {
		return "", Dono{}, fmt.Errorf("reserva: gerar token de carrinho: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, donoDe(token), nil
}

// DonoDoToken aceita só tokens no formato emitido (base64url de 32 bytes).
func DonoDoToken(token string) (Dono, bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != tamanhoToken {
		return Dono{}, false
	}
	return donoDe(token), true
}

func donoDe(token string) Dono {
	soma := sha256.Sum256([]byte(token))
	return Dono{hash: soma[:]}
}

// chaveTrava deriva a chave do advisory lock do dono (colisão só serializa
// dois donos distintos — inofensivo).
func (d Dono) chaveTrava() int32 {
	return int32(binary.BigEndian.Uint32(d.hash[:4])) //nolint:gosec // reinterpretação intencional dos bits
}

// chaveLimite identifica o dono no limitador (hash em hex; nunca o token).
func (d Dono) chaveLimite() string { return fmt.Sprintf("%x", d.hash) }
