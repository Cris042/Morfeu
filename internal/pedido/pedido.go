// Package pedido é o módulo do checkout (doc.md fluxo crítico 1, ADR 0010):
// o aggregate Pedido e sua máquina de estados (DDD tático — ADR 0005), a
// criação com os holds presos na mesma TX e a cobrança pela porta
// pagamento.Gateway. Não importa outros módulos: sessões, reserva e
// limitadores chegam por interfaces pequenas ligadas no main (ADR 0003).
package pedido

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Regras do checkout (ADR 0010, refinamento E6).
const (
	MaxAssentos = 6
	TTLPedido   = 15 * time.Minute
	MargemHold  = 2 * time.Minute // o hold preso vence depois do pedido
	Moeda       = "brl"
	maxEmail    = 254
	bytesCodigo = 10 // 80 bits → 16 caracteres base32
)

var formatoAssento = regexp.MustCompile(`^[A-Z][1-9][0-9]?$`)

// Pedido é o aggregate do checkout. Campos não exportados: só nasce válido
// por NovoPedido (total sempre calculado aqui, nunca vindo do cliente).
type Pedido struct {
	id            uuid.UUID
	codigo        string
	email         string
	donoHash      []byte
	sessaoID      int64
	assentos      []string
	totalCentavos int64
	status        Status
	expiraEm      time.Time
}

// Entrada é o que o cliente envia (o e-mail é sempre obrigatório — fluxo
// único convidado+conta, doc.md §10).
type Entrada struct {
	Email    string
	SessaoID int64
	Assentos []string
}

// validar confere a entrada em memória antes de qualquer I/O.
func (in Entrada) validar() error {
	var campos []string
	if !emailValido(in.Email) {
		campos = append(campos, "email")
	}
	if in.SessaoID <= 0 {
		campos = append(campos, "sessao_id")
	}
	if !assentosValidos(in.Assentos) {
		campos = append(campos, "assentos")
	}
	if len(campos) > 0 {
		return &ErroValidacao{Campos: campos}
	}
	return nil
}

func emailValido(e string) bool {
	if len(e) == 0 || len(e) > maxEmail || strings.TrimSpace(e) != e {
		return false
	}
	a, err := mail.ParseAddress(e)
	return err == nil && a.Address == e && a.Name == ""
}

func assentosValidos(as []string) bool {
	if len(as) == 0 || len(as) > MaxAssentos {
		return false
	}
	vistos := make(map[string]bool, len(as))
	for _, a := range as {
		if !formatoAssento.MatchString(a) || vistos[a] {
			return false
		}
		vistos[a] = true
	}
	return true
}

// NovoPedido cria o pedido aguardando pagamento: total = preço da sessão ×
// assentos (em centavos), prazo de 15 min, código público aleatório.
func NovoPedido(in Entrada, donoHash []byte, precoCentavos int64, agora time.Time) (Pedido, error) {
	if err := in.validar(); err != nil {
		return Pedido{}, err
	}
	if precoCentavos <= 0 {
		return Pedido{}, fmt.Errorf("pedido: preço da sessão inválido (%d)", precoCentavos)
	}
	codigo, err := novoCodigo()
	if err != nil {
		return Pedido{}, err
	}
	assentos := append([]string(nil), in.Assentos...)
	return Pedido{
		id: uuid.New(), codigo: codigo, email: in.Email, donoHash: donoHash, sessaoID: in.SessaoID,
		assentos: assentos, totalCentavos: precoCentavos * int64(len(assentos)),
		status: AguardandoPagamento, expiraEm: agora.Add(TTLPedido),
	}, nil
}

// novoCodigo gera o código público do pedido: não sequencial, ≥ 64 bits
// (refinamento E6, security) — é metade da credencial da consulta de
// convidado no E8 (e-mail + código).
func novoCodigo() (string, error) {
	b := make([]byte, bytesCodigo)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("pedido: gerar código: %w", err)
	}
	return base32.StdEncoding.EncodeToString(b), nil
}

// ID do pedido.
func (p Pedido) ID() uuid.UUID { return p.id }

// Codigo público do pedido.
func (p Pedido) Codigo() string { return p.codigo }

// SessaoID da sessão comprada.
func (p Pedido) SessaoID() int64 { return p.sessaoID }

// Assentos pedidos.
func (p Pedido) Assentos() []string { return append([]string(nil), p.assentos...) }

// TotalCentavos calculado no servidor.
func (p Pedido) TotalCentavos() int64 { return p.totalCentavos }

// Status atual.
func (p Pedido) Status() Status { return p.status }

// ExpiraEm é o fim do prazo para pagar.
func (p Pedido) ExpiraEm() time.Time { return p.expiraEm }

// HoldAte é até quando os holds ficam presos ao pedido.
func (p Pedido) HoldAte() time.Time { return p.expiraEm.Add(MargemHold) }
