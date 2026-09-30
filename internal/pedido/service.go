package pedido

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/outbox"
	"github.com/mclovin137/morfeu/internal/pedido/db"
	"github.com/mclovin137/morfeu/internal/pedido/pagamento"
)

// FonteSessoes é a porta para o módulo sessao (RF03): preço por assento de
// uma sessão aberta. ok=false = sessão indisponível.
type FonteSessoes interface {
	PrecoDaSessaoAberta(ctx context.Context, sessaoID int64) (centavos int64, ok bool, err error)
}

// Reserva é a porta transacional para o módulo reserva (ADR 0010): os
// métodos recebem a TX aberta por este serviço — o único dono dela. O
// adapter do main traduz os erros da reserva para ErrHoldsInvalidos.
type Reserva interface {
	DonoDoToken(token string) (donoHash []byte, ok bool)
	PrenderParaPedido(ctx context.Context, tx outbox.Tx, donoHash []byte, sessaoID int64, codigos []string, pedidoID uuid.UUID, ate time.Time) error
	LiberarDoPedido(ctx context.Context, tx outbox.Tx, pedidoID uuid.UUID) (int64, error)
	// ConverterDoPedido marca os holds do pedido como vendidos e devolve os
	// códigos convertidos (idempotente — PRD 0022 RF04).
	ConverterDoPedido(ctx context.Context, tx outbox.Tx, pedidoID uuid.UUID) ([]string, error)
}

// Limitador é o que o serviço usa do limitador da plataforma.
type Limitador interface {
	Bloqueado(ctx context.Context, chave string) bool
	RegistrarFalha(ctx context.Context, chave string)
}

// Etapas fixas do funil (sem labels de alta cardinalidade — refinamento E6).
const (
	EtapaPedidoCriado   = "pedido_criado"
	EtapaCobrancaCriada = "cobranca_criada"
	EtapaGatewayFalhou  = "gateway_falhou"
)

// Config agrupa as dependências injetadas pelo main.
type Config struct {
	Sessoes    FonteSessoes
	Reserva    Reserva
	Gateway    pagamento.Gateway
	LimiteIP   Limitador // criação: 10/min por IP
	LimiteDono Limitador // criação: 5/min por carrinho
	// Webhook verifica os eventos do gateway; nil = rota do webhook desligada
	// (sem segredo configurado).
	Webhook *pagamento.Webhook
	// LimiteWebhook é o teto folgado por IP da rota pública do webhook (só
	// contra DoS — o Stripe reenvia o que receber 429).
	LimiteWebhook Limitador
	Funil         func(ctx context.Context, etapa string)
	Agora         func() time.Time
}

// Servico implementa os casos de uso do pedido.
type Servico struct {
	pool   outbox.Pool
	cfg    Config
	logger *zap.Logger
}

// NovoServico cria o serviço; portas, gateway e limitadores são obrigatórios.
func NovoServico(pool outbox.Pool, cfg Config, logger *zap.Logger) (*Servico, error) {
	if cfg.Sessoes == nil || cfg.Reserva == nil || cfg.Gateway == nil || cfg.LimiteIP == nil || cfg.LimiteDono == nil {
		return nil, errors.New("pedido: porta, gateway ou limitador ausente")
	}
	if cfg.Webhook != nil && cfg.LimiteWebhook == nil {
		return nil, errors.New("pedido: webhook sem limitador")
	}
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	if cfg.Funil == nil {
		cfg.Funil = func(context.Context, string) {}
	}
	return &Servico{pool: pool, cfg: cfg, logger: logger}, nil
}

// DonoDoToken identifica o carrinho pelo token do cookie (via reserva).
func (s *Servico) DonoDoToken(token string) ([]byte, bool) { return s.cfg.Reserva.DonoDoToken(token) }

// ContarRequisicao aplica o rate limit da criação (RF07).
func (s *Servico) ContarRequisicao(ctx context.Context, ip string, donoHash []byte) error {
	dono := fmt.Sprintf("%x", donoHash)
	if s.cfg.LimiteIP.Bloqueado(ctx, ip) || s.cfg.LimiteDono.Bloqueado(ctx, dono) {
		return ErrMuitasRequisicoes
	}
	s.cfg.LimiteIP.RegistrarFalha(ctx, ip)
	s.cfg.LimiteDono.RegistrarFalha(ctx, dono)
	return nil
}

// ContarWebhook aplica o teto folgado por IP da rota do webhook.
func (s *Servico) ContarWebhook(ctx context.Context, ip string) error {
	if s.cfg.LimiteWebhook.Bloqueado(ctx, ip) {
		return ErrMuitasRequisicoes
	}
	s.cfg.LimiteWebhook.RegistrarFalha(ctx, ip)
	return nil
}

// Criado é o resultado da criação: o pedido e o segredo do cliente da
// cobrança (vai só para o navegador do dono, que confirma o pagamento).
type Criado struct {
	Pedido         Pedido
	SegredoCliente string
}

// Criar abre o pedido (RF01–RF05): valida → preço pela porta → TX (expira o
// pendente vencido do carrinho, insere, prende os holds) → cobrança no
// gateway FORA da TX. Falha do gateway desfaz: pedido falhou + holds livres.
func (s *Servico) Criar(ctx context.Context, donoHash []byte, in Entrada) (Criado, error) {
	if err := in.validar(); err != nil {
		return Criado{}, err
	}
	preco, ok, err := s.cfg.Sessoes.PrecoDaSessaoAberta(ctx, in.SessaoID)
	if err != nil {
		return Criado{}, fmt.Errorf("pedido: consultar sessão: %w", err)
	}
	if !ok {
		return Criado{}, ErrSessaoIndisponivel
	}
	agora := s.cfg.Agora()
	p, err := NovoPedido(in, donoHash, preco, agora)
	if err != nil {
		return Criado{}, err
	}
	if err := s.expirarPendenteVencido(ctx, donoHash, agora); err != nil {
		return Criado{}, err
	}
	err = outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		return s.abrirNaTx(ctx, tx, p, agora)
	})
	if err != nil {
		return Criado{}, err
	}
	s.cfg.Funil(ctx, EtapaPedidoCriado)

	intencao, err := s.cfg.Gateway.CriarCobranca(ctx, pagamento.Cobranca{
		PedidoID: p.id, ValorCentavos: p.totalCentavos, Moeda: Moeda,
		ChaveIdempotencia: "pedido-" + p.id.String() + "-cobranca",
	})
	if err != nil {
		s.cfg.Funil(ctx, EtapaGatewayFalhou)
		s.logger.Warn("pedido: gateway recusou a cobrança", zap.String("pedido_id", p.id.String()), zap.Error(err))
		ctxDesfazer, cancelar := ctxPosCobranca(ctx)
		defer cancelar()
		if errDesfazer := s.desfazer(ctxDesfazer, p.id); errDesfazer != nil {
			s.logger.Error("pedido: desfazer após falha do gateway", zap.String("pedido_id", p.id.String()), zap.Error(errDesfazer))
		}
		return Criado{}, ErrGatewayIndisponivel
	}
	ctxGravar, cancelar := ctxPosCobranca(ctx)
	defer cancelar()
	if err := (repositorio{q: db.New(s.pool)}).definirCobranca(ctxGravar, p.id, intencao.ID, s.cfg.Agora()); err != nil {
		return Criado{}, err
	}
	s.cfg.Funil(ctx, EtapaCobrancaCriada)
	s.logger.Info("pedido: criado", zap.String("pedido_id", p.id.String()), zap.Int64("sessao_id", p.sessaoID), zap.Int("assentos", len(p.assentos)))
	return Criado{Pedido: p, SegredoCliente: intencao.SegredoCliente}, nil
}

// expirarPendenteVencido expira (lazy) o pedido vencido do carrinho e
// devolve os assentos, numa TX própria: se a abertura do novo pedido falhar
// (ex.: os holds precisam ser refeitos), o carrinho não volta a ficar preso
// ao pedido vencido. A reconciliação (task 0025) cancela a cobrança dele.
func (s *Servico) expirarPendenteVencido(ctx context.Context, donoHash []byte, agora time.Time) error {
	return outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		r := repositorio{q: db.New(tx)}
		pendentes, err := r.pendenteDoDono(ctx, donoHash)
		if err != nil {
			return err
		}
		for _, pe := range pendentes {
			if pe.expiraEm.After(agora) {
				continue
			}
			if err := s.expirar(ctx, tx, r, pe.id, agora); err != nil {
				return err
			}
		}
		return nil
	})
}

// expirar vence o pedido (CAS) e devolve os assentos na TX recebida. Se outro
// caminho (webhook/reconciliação) já o transicionou, não há o que fazer.
func (s *Servico) expirar(ctx context.Context, tx outbox.Tx, r repositorio, id uuid.UUID, agora time.Time) error {
	_, err := r.transicionar(ctx, id, AguardandoPagamento, PrazoVencido, agora)
	if errors.Is(err, ErrTransicaoConcorrente) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.cfg.Reserva.LiberarDoPedido(ctx, tx, id)
	return err
}

// prazoPosCobranca limita os passos que seguem a chamada ao gateway.
const prazoPosCobranca = 5 * time.Second

// ctxPosCobranca desacopla do cancelamento da requisição os passos que
// seguem a chamada ao gateway (gravar a cobrança ou desfazer o pedido): se o
// cliente desistir no meio, o estado do pedido ainda fecha (auditoria 0023).
func ctxPosCobranca(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), prazoPosCobranca)
}

// abrirNaTx insere o pedido e prende os holds — tudo ou nada. Pedido
// pendente ainda no prazo bloqueia o carrinho (1 por vez).
func (s *Servico) abrirNaTx(ctx context.Context, tx outbox.Tx, p Pedido, agora time.Time) error {
	r := repositorio{q: db.New(tx)}
	pendentes, err := r.pendenteDoDono(ctx, p.donoHash)
	if err != nil {
		return err
	}
	for _, pe := range pendentes {
		if pe.expiraEm.After(agora) {
			return &ErroPendente{PedidoID: pe.id.String()}
		}
	}
	inseriu, err := r.inserir(ctx, p, agora)
	if err != nil {
		return err
	}
	if !inseriu {
		// Corrida com outra criação do mesmo carrinho: a outra venceu.
		return ErrPedidoPendente
	}
	return s.cfg.Reserva.PrenderParaPedido(ctx, tx, p.donoHash, p.sessaoID, p.assentos, p.id, p.HoldAte())
}

// desfazer marca o pedido como falhou e devolve os assentos (a cobrança não
// existe). Se outro caminho já o transicionou, não há o que desfazer.
func (s *Servico) desfazer(ctx context.Context, id uuid.UUID) error {
	return outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		r := repositorio{q: db.New(tx)}
		if _, err := r.transicionar(ctx, id, AguardandoPagamento, CobrancaFalhou, s.cfg.Agora()); err != nil {
			if errors.Is(err, ErrTransicaoConcorrente) {
				return nil
			}
			return err
		}
		_, err := s.cfg.Reserva.LiberarDoPedido(ctx, tx, id)
		return err
	})
}

// Obter devolve o pedido só para o carrinho que o criou (RF06).
func (s *Servico) Obter(ctx context.Context, id uuid.UUID, donoHash []byte) (Visao, error) {
	v, ok, err := repositorio{q: db.New(s.pool)}.doDono(ctx, id, donoHash)
	if err != nil {
		return Visao{}, err
	}
	if !ok {
		return Visao{}, ErrPedidoNaoEncontrado
	}
	return v, nil
}
