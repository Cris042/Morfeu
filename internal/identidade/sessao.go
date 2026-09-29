package identidade

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/autenticacao"
	"github.com/mclovin137/morfeu/internal/identidade/db"
	"github.com/mclovin137/morfeu/internal/outbox"
)

// TTLRefresh é a validade do refresh token (decisão do usuário no
// refinamento E1: 7 dias).
const TTLRefresh = 7 * 24 * time.Hour

// tamanhoRefresh: 32 bytes de crypto/rand = 256 bits de entropia (RF02).
const tamanhoRefresh = 32

var (
	// ErrRefreshInvalido: token ausente, desconhecido, expirado ou reusado —
	// o cliente sempre recebe o mesmo 401.
	ErrRefreshInvalido = errors.New("identidade: refresh token inválido")
	// ErrContaNaoRemovivel: só cliente remove a própria conta pela API.
	ErrContaNaoRemovivel = errors.New("identidade: conta não removível")
)

// Refresh é o token emitido para o cookie (valor em claro só aqui — no banco
// fica o SHA-256).
type Refresh struct {
	Token    string
	ExpiraEm time.Time
}

// novoRefresh gera um token opaco e seu hash.
func novoRefresh() (claro string, hash []byte, err error) {
	b := make([]byte, tamanhoRefresh)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("identidade: gerar refresh: %w", err)
	}
	claro = base64.RawURLEncoding.EncodeToString(b)
	return claro, hashRefresh(claro), nil
}

func hashRefresh(claro string) []byte {
	soma := sha256.Sum256([]byte(claro))
	return soma[:]
}

// emitirRefresh grava o refresh da família (nova no login, a mesma na
// rotação) usando q — dentro ou fora de uma TX.
func (s *Servico) emitirRefresh(ctx context.Context, q *db.Queries, usuarioID, familiaID uuid.UUID) (Refresh, error) {
	claro, hash, err := novoRefresh()
	if err != nil {
		return Refresh{}, err
	}
	expira := s.agora().Add(TTLRefresh)
	if err := q.InserirRefresh(ctx, db.InserirRefreshParams{
		ID: uuid.New(), UsuarioID: usuarioID, FamiliaID: familiaID, Hash: hash, ExpiraEm: expira,
	}); err != nil {
		return Refresh{}, fmt.Errorf("identidade: gravar refresh: %w", err)
	}
	return Refresh{Token: claro, ExpiraEm: expira}, nil
}

// Renovar troca o refresh apresentado por um novo access + um novo refresh
// da mesma família (RF04). Tudo numa TX com o token travado (FOR UPDATE):
// dois refreshes concorrentes serializam e o segundo vira reuso (RF05).
// Reuso revoga a família inteira — a revogação é COMMITADA mesmo com o
// chamador recebendo erro.
func (s *Servico) Renovar(ctx context.Context, tokenClaro string) (Sessao, error) {
	if tokenClaro == "" {
		return Sessao{}, ErrRefreshInvalido
	}
	var r rotacao
	err := outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		var err error
		r, err = s.rotacionar(ctx, db.New(tx), tokenClaro)
		return err
	})
	if err != nil {
		return Sessao{}, err
	}
	if r.reuso {
		s.metricas.ReusoRefresh(ctx)
		s.logger.Warn("refresh_reuso_detectado", zap.String("familia_id", r.familia.String()))
		return Sessao{}, ErrRefreshInvalido
	}
	return r.sessao, nil
}

// rotacao é o resultado do passo transacional do refresh.
type rotacao struct {
	sessao  Sessao
	reuso   bool
	familia uuid.UUID
}

// rotacionar roda dentro da TX com o token travado. Reuso devolve erro nil
// (a revogação da família precisa ser commitada) e reuso=true.
func (s *Servico) rotacionar(ctx context.Context, q *db.Queries, tokenClaro string) (rotacao, error) {
	linhas, err := q.TravarRefreshPorHash(ctx, hashRefresh(tokenClaro))
	if err != nil {
		return rotacao{}, fmt.Errorf("identidade: travar refresh: %w", err)
	}
	if len(linhas) == 0 {
		return rotacao{}, ErrRefreshInvalido
	}
	atual := linhas[0]
	if atual.UsadoEm != nil || atual.RevogadoEm != nil {
		if _, err := q.RevogarFamilia(ctx, atual.FamiliaID); err != nil {
			return rotacao{}, fmt.Errorf("identidade: revogar família: %w", err)
		}
		return rotacao{reuso: true, familia: atual.FamiliaID}, nil
	}
	if !s.agora().Before(atual.ExpiraEm) {
		return rotacao{}, ErrRefreshInvalido
	}
	if err := q.MarcarRefreshUsado(ctx, atual.ID); err != nil {
		return rotacao{}, fmt.Errorf("identidade: marcar refresh usado: %w", err)
	}
	novo, err := s.emitirRefresh(ctx, q, atual.UsuarioID, atual.FamiliaID)
	if err != nil {
		return rotacao{}, err
	}
	access, expira, err := s.emissor.Emitir(atual.UsuarioID, autenticacao.Papel(atual.Papel))
	if err != nil {
		return rotacao{}, err
	}
	return rotacao{sessao: Sessao{AccessToken: access, ExpiraEm: expira, Refresh: novo}}, nil
}

// Encerrar revoga a família do refresh apresentado (logout, RF06).
// Idempotente: token vazio ou desconhecido não é erro.
func (s *Servico) Encerrar(ctx context.Context, tokenClaro string) error {
	if tokenClaro == "" {
		return nil
	}
	if _, err := s.q.RevogarFamiliaPorHash(ctx, hashRefresh(tokenClaro)); err != nil {
		return fmt.Errorf("identidade: revogar família no logout: %w", err)
	}
	return nil
}

// RemoverConta pseudonimiza o cliente e revoga todas as suas famílias numa
// TX (RF07). O id é preservado (FKs futuras de pedido); nome/e-mail viram
// valores fixos sem PII e a senha deixa de casar com qualquer entrada.
func (s *Servico) RemoverConta(ctx context.Context, usuarioID uuid.UUID) error {
	return outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		q := db.New(tx)
		n, err := q.PseudonimizarUsuario(ctx, usuarioID)
		if err != nil {
			return fmt.Errorf("identidade: pseudonimizar: %w", err)
		}
		if n == 0 {
			return ErrContaNaoRemovivel
		}
		if _, err := q.RevogarTodasDoUsuario(ctx, usuarioID); err != nil {
			return fmt.Errorf("identidade: revogar sessões: %w", err)
		}
		s.logger.Info("conta removida (pseudonimizada)", zap.String("usuario_id", usuarioID.String()))
		return nil
	})
}

// LimparRefreshExpirados apaga refresh vencidos (RF08) — chamado
// periodicamente pelo worker.
func LimparRefreshExpirados(ctx context.Context, q *db.Queries) (int64, error) {
	n, err := q.ApagarRefreshExpirados(ctx)
	if err != nil {
		return 0, fmt.Errorf("identidade: limpar refresh expirados: %w", err)
	}
	return n, nil
}
