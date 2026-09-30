package pedido

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/mclovin137/morfeu/internal/pedido/db"
)

// ErrNaoNotificavel: o pedido não está pago ou não tem ingresso ativo — nada
// a enviar (nunca um QR de ingresso inválido; refinamento E7, security).
var ErrNaoNotificavel = errors.New("pedido: não notificável (não pago ou sem ingresso ativo)")

// contextoToken separa o domínio do HMAC: a mesma chave nunca assina outra
// coisa com o mesmo formato (refinamento E7, security).
const contextoToken = "ingresso:v1:" //nolint:gosec // prefixo de domínio do HMAC, não é credencial

// TamanhoMinimoSegredoToken: 32 bytes (256 bits) por versão.
const TamanhoMinimoSegredoToken = 32

// TokenIngresso é o token opaco do ingresso (ADR 0010): HMAC-SHA256 do id com
// o segredo da versão, em base64url sem padding (43 caracteres, 256 bits).
// Nada em claro no banco: o token é recalculado quando preciso.
func TokenIngresso(segredo []byte, ingressoID uuid.UUID) string {
	mac := hmac.New(sha256.New, segredo)
	mac.Write([]byte(contextoToken + ingressoID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// IngressoNotificado é um ingresso ativo com o token já calculado.
type IngressoNotificado struct {
	ID      uuid.UUID
	Assento string
	Token   string
}

// DadosNotificacao é o que a notificação precisa do pedido (porta — PRD 0028).
type DadosNotificacao struct {
	Email         string
	Codigo        string
	SessaoID      int64
	TotalCentavos int64
	Ingressos     []IngressoNotificado
}

// DadosParaNotificacao devolve o pedido pago com os ingressos ativos e seus
// tokens. O segredo nunca sai do módulo. Pedido inexistente →
// ErrPedidoNaoEncontrado; não pago ou sem ingresso ativo → ErrNaoNotificavel.
func (s *Servico) DadosParaNotificacao(ctx context.Context, id uuid.UUID) (DadosNotificacao, error) {
	q := db.New(s.pool)
	linhas, err := q.PedidoParaNotificacao(ctx, id)
	if err != nil {
		return DadosNotificacao{}, fmt.Errorf("pedido: dados para notificação: %w", err)
	}
	if len(linhas) == 0 {
		return DadosNotificacao{}, ErrPedidoNaoEncontrado
	}
	p := linhas[0]
	if Status(p.Status) != Pago {
		return DadosNotificacao{}, ErrNaoNotificavel
	}
	ingressos, err := q.IngressosAtivosDoPedido(ctx, id)
	if err != nil {
		return DadosNotificacao{}, fmt.Errorf("pedido: ingressos do pedido: %w", err)
	}
	if len(ingressos) == 0 {
		return DadosNotificacao{}, ErrNaoNotificavel
	}
	out := DadosNotificacao{Email: p.Email, Codigo: p.Codigo, SessaoID: p.SessaoID, TotalCentavos: p.TotalCentavos}
	for _, i := range ingressos {
		segredo, ok := s.cfg.SegredosToken[i.VersaoToken]
		if !ok {
			return DadosNotificacao{}, fmt.Errorf("pedido: sem segredo para a versão %d do token", i.VersaoToken)
		}
		out.Ingressos = append(out.Ingressos, IngressoNotificado{ID: i.ID, Assento: i.AssentoCodigo, Token: TokenIngresso(segredo, i.ID)})
	}
	return out, nil
}

// DadosAvisoEstorno é o que o e-mail de estorno mostra (sem ingresso, sem token).
type DadosAvisoEstorno struct {
	Email         string
	Codigo        string
	TotalCentavos int64
}

// DadosParaAvisoDeEstorno é a porta do e-mail de estorno (PRD 0030): só
// pedido estornado. Inexistente → ErrPedidoNaoEncontrado; qualquer outro
// status (inclusive estorno_pendente) → ErrNaoNotificavel.
func (s *Servico) DadosParaAvisoDeEstorno(ctx context.Context, id uuid.UUID) (DadosAvisoEstorno, error) {
	linhas, err := db.New(s.pool).PedidoParaNotificacao(ctx, id)
	if err != nil {
		return DadosAvisoEstorno{}, fmt.Errorf("pedido: dados do aviso de estorno: %w", err)
	}
	if len(linhas) == 0 {
		return DadosAvisoEstorno{}, ErrPedidoNaoEncontrado
	}
	p := linhas[0]
	if Status(p.Status) != Estornado {
		return DadosAvisoEstorno{}, ErrNaoNotificavel
	}
	return DadosAvisoEstorno{Email: p.Email, Codigo: p.Codigo, TotalCentavos: p.TotalCentavos}, nil
}
