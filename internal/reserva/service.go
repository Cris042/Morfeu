// Package reserva é o módulo dono da trava de assentos (doc.md fluxo crítico
// 2, ADR 0008): holds com índice único parcial, expiração lazy, extensão
// única, teto por dono e sweeper de higiene. DDD tático (ADR 0005). Não lê
// sessoes/salas (os assentos válidos vêm pela porta FonteSessoes) nem conhece
// autenticação ou OTel — tudo injetado pelo main (ADR 0003).
package reserva

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/cache"
	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/reserva/db"
)

// maxTentativasImpasse: repetições da TX de trava em deadlock (40P01).
const maxTentativasImpasse = 3

// FonteSessoes é a porta para o sessao (RF02): códigos dos assentos de uma
// sessão agendada que ainda não começou. ok=false = sessão indisponível.
type FonteSessoes interface {
	AssentosDaSessaoAberta(ctx context.Context, sessaoID int64) (codigos []string, ok bool, err error)
}

// Limitador é o que o serviço usa do limitador da plataforma
// (autenticacao.Limitador) — interface no consumidor (ADR 0003).
type Limitador interface {
	Bloqueado(ctx context.Context, chave string) bool
	RegistrarFalha(ctx context.Context, chave string)
}

// Metricas são callbacks do main (o domínio não conhece OTel — RF10).
type Metricas struct {
	Criados       func(ctx context.Context, n int64)
	Indisponiveis func(ctx context.Context)
	Expirados     func(ctx context.Context, n int64)
	Varreduras    func(ctx context.Context)
	Convertidos   func(ctx context.Context, n int64) // vendidos (PRD 0022 RF09)
}

// comPadroes troca callbacks ausentes por no-op.
func (m Metricas) comPadroes() Metricas {
	if m.Criados == nil {
		m.Criados = func(context.Context, int64) {}
	}
	if m.Indisponiveis == nil {
		m.Indisponiveis = func(context.Context) {}
	}
	if m.Expirados == nil {
		m.Expirados = func(context.Context, int64) {}
	}
	if m.Varreduras == nil {
		m.Varreduras = func(context.Context) {}
	}
	if m.Convertidos == nil {
		m.Convertidos = func(context.Context, int64) {}
	}
	return m
}

// Config agrupa as dependências injetadas pelo main.
type Config struct {
	Sessoes    FonteSessoes
	LimiteIP   Limitador // criar/estender: 30/min por IP (RF08)
	LimiteDono Limitador // criar/estender: 20/min por dono (RF08)
	Metricas   Metricas
	Agora      func() time.Time
	Cache      cache.Cache // opcional (PRD 0016): ocupação pública, TTL 3 s
}

// Servico implementa os casos de uso da trava.
type Servico struct {
	pool   outbox.Pool
	cfg    Config
	logger *zap.Logger
}

// NovoServico cria o serviço; porta de sessões e limitadores são obrigatórios.
func NovoServico(pool outbox.Pool, cfg Config, logger *zap.Logger) (*Servico, error) {
	if cfg.Sessoes == nil || cfg.LimiteIP == nil || cfg.LimiteDono == nil {
		return nil, errors.New("reserva: porta de sessões ou limitadores ausentes")
	}
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	cfg.Metricas = cfg.Metricas.comPadroes()
	return &Servico{pool: pool, cfg: cfg, logger: logger}, nil
}

// ContarRequisicao aplica o rate limit de criar/estender (RF08): por IP
// sempre; por dono quando a requisição já traz um carrinho válido.
func (s *Servico) ContarRequisicao(ctx context.Context, ip string, d Dono, temDono bool) error {
	if s.cfg.LimiteIP.Bloqueado(ctx, ip) || (temDono && s.cfg.LimiteDono.Bloqueado(ctx, d.chaveLimite())) {
		return ErrMuitasRequisicoes
	}
	s.cfg.LimiteIP.RegistrarFalha(ctx, ip)
	if temDono {
		s.cfg.LimiteDono.RegistrarFalha(ctx, d.chaveLimite())
	}
	return nil
}

// Travar trava o lote inteiro para o dono ou nada (RF04).
func (s *Servico) Travar(ctx context.Context, sessaoID int64, codigos []string, d Dono) ([]Hold, error) {
	lote, err := NovoLote(codigos)
	if err != nil {
		return nil, err
	}
	daSala, ok, err := s.cfg.Sessoes.AssentosDaSessaoAberta(ctx, sessaoID)
	if err != nil {
		return nil, fmt.Errorf("reserva: consultar sessão: %w", err)
	}
	if !ok {
		return nil, ErrSessaoIndisponivel
	}
	if err := lote.ContidoEm(daSala); err != nil {
		return nil, err
	}
	var holds []Hold
	var criados int
	for tentativa := 1; ; tentativa++ {
		agora := s.cfg.Agora()
		err = outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
			var errTx error
			holds, criados, errTx = travarNaTx(ctx, repositorio{q: db.New(tx)}, sessaoID, lote, d, agora)
			return errTx
		})
		if !db.EhImpasse(err) || tentativa == maxTentativasImpasse {
			break
		}
		s.logger.Warn("reserva: impasse na trava, repetindo", zap.Int("tentativa", tentativa))
	}
	var ind *ErroIndisponivel
	switch {
	case errors.As(err, &ind):
		s.cfg.Metricas.Indisponiveis(ctx)
		s.logger.Info("reserva: trava recusada", zap.Int64("sessao_id", sessaoID), zap.Int("recusados", len(ind.Assentos)))
		return nil, err
	case err != nil:
		if codigo := db.CodigoSQL(err); codigo != "" {
			s.logger.Error("reserva: erro do banco na trava", zap.String("sqlstate", codigo))
		}
		return nil, err
	}
	s.cfg.Metricas.Criados(ctx, int64(criados))
	s.logger.Info("reserva: assentos travados", zap.Int64("sessao_id", sessaoID), zap.Int("quantidade", len(holds)))
	return holds, nil
}

// travarNaTx: advisory lock do dono → teto → upserts em ordem crescente.
// Assentos que o próprio dono já trava nesta sessão voltam como estão.
func travarNaTx(ctx context.Context, r repositorio, sessaoID int64, lote Lote, d Dono, agora time.Time) ([]Hold, int, error) {
	if err := r.travarDono(ctx, d); err != nil {
		return nil, 0, err
	}
	vivos, err := r.vivosDoDono(ctx, d, agora)
	if err != nil {
		return nil, 0, err
	}
	meus := map[AssentoCodigo]Hold{}
	for _, h := range vivos {
		if h.sessaoID == sessaoID {
			meus[h.assento] = h
		}
	}
	var novos []AssentoCodigo
	out := make([]Hold, 0, len(lote.assentos))
	for _, a := range lote.assentos {
		if h, ok := meus[a]; ok {
			out = append(out, h)
			continue
		}
		novos = append(novos, a)
	}
	if len(vivos)+len(novos) > MaxHoldsPorDono {
		return nil, 0, ErrLimiteHolds
	}
	var recusados []string
	for _, a := range novos {
		h, ok, err := r.travar(ctx, sessaoID, a, d, agora)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			recusados = append(recusados, string(a))
			continue
		}
		out = append(out, h)
	}
	if len(recusados) > 0 {
		return nil, 0, &ErroIndisponivel{Assentos: recusados}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].assento < out[j].assento })
	return out, len(novos), nil
}

// MeusHolds lista os holds vivos do dono (RF05).
func (s *Servico) MeusHolds(ctx context.Context, d Dono) ([]Hold, error) {
	return repositorio{q: db.New(s.pool)}.vivosDoDono(ctx, d, s.cfg.Agora())
}

// Estender aplica a única extensão (RF06): aggregate primeiro, UPDATE com
// guarda depois.
func (s *Servico) Estender(ctx context.Context, id uuid.UUID, d Dono) (Hold, error) {
	r := repositorio{q: db.New(s.pool)}
	agora := s.cfg.Agora()
	h, ok, err := r.vivoDoDono(ctx, id, d, agora)
	if err != nil {
		return Hold{}, err
	}
	if !ok {
		return Hold{}, ErrHoldNaoEncontrado
	}
	estendido, err := h.Estender(agora)
	if err != nil {
		return Hold{}, err
	}
	gravou, err := r.estender(ctx, estendido, d, agora)
	if err != nil {
		return Hold{}, err
	}
	if !gravou {
		// Outra requisição estendeu (ou o prazo venceu) entre a leitura e o UPDATE.
		if atual, vivo, errR := r.vivoDoDono(ctx, id, d, s.cfg.Agora()); errR == nil && vivo {
			if atual.EmPedido() {
				return Hold{}, ErrHoldEmPedido
			}
			return Hold{}, ErrExtensaoEsgotada
		}
		return Hold{}, ErrHoldNaoEncontrado
	}
	s.logger.Info("reserva: hold estendido", zap.Int64("sessao_id", h.sessaoID))
	return estendido, nil
}

// Liberar devolve o assento (RF07).
func (s *Servico) Liberar(ctx context.Context, id uuid.UUID, d Dono) error {
	r := repositorio{q: db.New(s.pool)}
	ok, err := r.liberar(ctx, id, d, s.cfg.Agora())
	if err != nil {
		return err
	}
	if !ok {
		// Distingue o hold preso a um pedido (409) do inexistente/alheio (404).
		if h, vivo, errR := r.vivoDoDono(ctx, id, d, s.cfg.Agora()); errR == nil && vivo && h.EmPedido() {
			return ErrHoldEmPedido
		}
		return ErrHoldNaoEncontrado
	}
	s.logger.Info("reserva: hold liberado")
	return nil
}
