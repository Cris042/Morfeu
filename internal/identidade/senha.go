package identidade

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ParametrosArgon2 são os custos do Argon2id (RF06 do PRD 0009). Padrão =
// mínimo OWASP (m=19 MiB, t=2, p=1); configuráveis por env para recalibrar
// na VM A1 (checklist da E0c-CD).
type ParametrosArgon2 struct {
	MemoriaKiB  uint32
	Iteracoes   uint32
	Paralelismo uint8
}

// ParametrosArgon2Padrao: mínimo OWASP vigente.
var ParametrosArgon2Padrao = ParametrosArgon2{MemoriaKiB: 19 * 1024, Iteracoes: 2, Paralelismo: 1}

const (
	tamanhoSalt  = 16
	tamanhoChave = 32
)

var errHashMalformado = errors.New("identidade: hash de senha malformado")

// gerarHash devolve o hash no formato PHC:
// $argon2id$v=19$m=<KiB>,t=<n>,p=<n>$<salt>$<hash> (base64 sem padding).
func gerarHash(senha string, p ParametrosArgon2) (string, error) {
	salt := make([]byte, tamanhoSalt)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identidade: gerar salt: %w", err)
	}
	chave := argon2.IDKey([]byte(senha), salt, p.Iteracoes, p.MemoriaKiB, p.Paralelismo, tamanhoChave)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoriaKiB, p.Iteracoes, p.Paralelismo,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(chave)), nil
}

// verificarHash recalcula com os parâmetros GRAVADOS no próprio hash (rehash
// futuro sem migração) e compara em tempo constante.
func verificarHash(senha, codificado string) (bool, error) {
	partes := strings.Split(codificado, "$")
	if len(partes) != 6 || partes[1] != "argon2id" {
		return false, errHashMalformado
	}
	var versao int
	if _, err := fmt.Sscanf(partes[2], "v=%d", &versao); err != nil || versao != argon2.Version {
		return false, errHashMalformado
	}
	var p ParametrosArgon2
	if _, err := fmt.Sscanf(partes[3], "m=%d,t=%d,p=%d", &p.MemoriaKiB, &p.Iteracoes, &p.Paralelismo); err != nil {
		return false, errHashMalformado
	}
	// Tetos iguais aos da config (config.validarArgon2): um hash adulterado no
	// banco com custo absurdo não vira DoS no login (auditoria 0009).
	if p.MemoriaKiB == 0 || p.MemoriaKiB > 1024*1024 || p.Iteracoes == 0 || p.Iteracoes > 10 || p.Paralelismo == 0 || p.Paralelismo > 16 {
		return false, errHashMalformado
	}
	salt, err := base64.RawStdEncoding.DecodeString(partes[4])
	if err != nil {
		return false, errHashMalformado
	}
	esperado, err := base64.RawStdEncoding.DecodeString(partes[5])
	if err != nil || len(esperado) == 0 {
		return false, errHashMalformado
	}
	calculado := argon2.IDKey([]byte(senha), salt, p.Iteracoes, p.MemoriaKiB, p.Paralelismo, uint32(len(esperado))) //nolint:gosec // G115: len de hash decodificado (32 B), sem overflow
	return subtle.ConstantTimeCompare(calculado, esperado) == 1, nil
}

// gerarSenhaAleatoria cria a senha inicial do operador (RF05): 24 caracteres
// alfanuméricos de crypto/rand, sem viés de módulo (rejeição).
func gerarSenhaAleatoria() (string, error) {
	const alfabeto = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	const tamanho = 24
	limite := byte(256 - 256%len(alfabeto))
	out := make([]byte, 0, tamanho)
	buf := make([]byte, 64)
	for len(out) < tamanho {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("identidade: gerar senha: %w", err)
		}
		for _, b := range buf {
			if b < limite && len(out) < tamanho {
				out = append(out, alfabeto[int(b)%len(alfabeto)])
			}
		}
	}
	return string(out), nil
}
