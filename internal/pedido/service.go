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
	// SegredosToken: segredo do HMAC do token do ingresso por versão
	// (ingressos.versao_token — ADR 0010, rotação). Só a porta da notificação
	// usa; o segredo nunca sai do módulo.
	SegredosToken map[int16][]byte
	// Compensacao conta cada compensação executada (saga_compensacoes_total{passo}).
	Compensacao func(ctx context.Context, passo string)
	Agora       func() time.Time
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
	if cfg.Compensacao == nil {
		cfg.Compensacao = func(context.Context, string) {}
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
// ao pedido vencido. Depois do commit, cancela a cobrança dele no gateway
// (melhor esforço: se falhar, a reconciliação/estorno cobrem o pagamento).
func (s *Servico) expirarPendenteVencido(ctx context.Context, donoHash []byte, agora time.Time) error {
	var cancelar []pendente
	err := outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		var err error
		cancelar, err = s.expirarVencidosDoDono(ctx, tx, donoHash, agora)
		return err
	})
	if err != nil {
		return err
	}
	for _, pe := range cancelar {
		s.cancelarCobranca(ctx, pe.id, *pe.intencao)
	}
	return nil
}

// expirarVencidosDoDono expira na TX os pendentes vencidos do carrinho e
// devolve os que têm cobrança, a cancelar depois do commit.
func (s *Servico) expirarVencidosDoDono(ctx context.Context, tx outbox.Tx, donoHash []byte, agora time.Time) ([]pendente, error) {
	r := repositorio{q: db.New(tx)}
	pendentes, err := r.pendenteDoDono(ctx, donoHash)
	if err != nil {
		return nil, err
	}
	var cancelar []pendente
	for _, pe := range pendentes {
		if pe.expiraEm.After(agora) {
			continue
		}
		expirou, err := s.expirar(ctx, tx, r, pe.id, agora)
		if err != nil {
			return nil, err
		}
		if expirou && pe.intencao != nil {
			cancelar = append(cancelar, pe)
		}
	}
	return cancelar, nil
}

// cancelarCobranca cancela a cobrança de um pedido vencido (melhor esforço):
// sucesso a encerra; falha deixa para a varredura de cobranças abertas.
func (s *Servico) cancelarCobranca(ctx context.Context, pedidoID uuid.UUID, intencaoID string) {
	ctx, fim := ctxPosCobranca(ctx)
	defer fim()
	if err := s.cfg.Gateway.CancelarCobranca(ctx, intencaoID); err != nil {
		s.logger.Warn("pedido: cancelar cobrança de pedido vencido", zap.Error(err))
		return
	}
	if err := s.encerrarCobranca(ctx, pedidoID); err != nil {
		s.logger.Warn("pedido: encerrar cobrança", zap.Error(err))
	}
}

// expirar vence o pedido (CAS) e devolve os assentos na TX recebida. Se outro
// caminho (webhook/reconciliação) já o transicionou, não há o que fazer
// (expirou = false).
func (s *Servico) expirar(ctx context.Context, tx outbox.Tx, r repositorio, id uuid.UUID, agora time.Time) (bool, error) {
	_, err := r.transicionar(ctx, id, AguardandoPagamento, PrazoVencido, agora)
	if errors.Is(err, ErrTransicaoConcorrente) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = s.cfg.Reserva.LiberarDoPedido(ctx, tx, id); err != nil {
		return false, err
	}
	// Abandono não é compensação: conta só no funil. Dentro da TX de quem
	// chamou — um rollback raro superestima o contador em 1.
	s.cfg.Funil(ctx, EtapaExpirado)
	return true, nil
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
	desfez := false
	err := outbox.WithTx(ctx, s.pool, func(tx outbox.Tx) error {
		r := repositorio{q: db.New(tx)}
		if _, err := r.transicionar(ctx, id, AguardandoPagamento, CobrancaFalhou, s.cfg.Agora()); err != nil {
			if errors.Is(err, ErrTransicaoConcorrente) {
				return nil
			}
			return err
		}
		desfez = true
		_, err := s.cfg.Reserva.LiberarDoPedido(ctx, tx, id)
		return err
	})
	if err == nil && desfez {
		s.cfg.Compensacao(ctx, PassoCobranca)
	}
	return err
}

// Presos conta os pedidos que a saga deveria ter resolvido (gauge
// pedidos_presos, PRD 0026): pendentes vencidos além da margem do hold e
// estornos pendentes.
func (s *Servico) Presos(ctx context.Context) (aguardandoVencido, estornoPendente int64, err error) {
	r, err := db.New(s.pool).ContarPresos(ctx, s.cfg.Agora().Add(-MargemHold))
	if err != nil {
		return 0, 0, fmt.Errorf("pedido: contar presos: %w", err)
	}
	return r.AguardandoVencido, r.EstornoPendente, nil
}

// Obter devolve o pedido ao carrinho que o criou ou à conta vinculada a ele
// (RF06 da 0023; PRD 0031). Pedido alheio → não encontrado (404, nunca 403).
func (s *Servico) Obter(ctx context.Context, id uuid.UUID, donoHash []byte, usuarioID *uuid.UUID) (Visao, error) {
	v, ok, err := repositorio{q: db.New(s.pool)}.doDono(ctx, id, donoHash, usuarioID)
	if err != nil {
		return Visao{}, err
	}
	if !ok {
		return Visao{}, ErrPedidoNaoEncontrado
	}
	return v, nil
}

// TamanhoPaginaPedidos: "Meus pedidos" em páginas de 20.
const TamanhoPaginaPedidos = 20

// MeusPedidos lista os pedidos da conta, mais recentes primeiro (PRD 0031).
func (s *Servico) MeusPedidos(ctx context.Context, usuarioID uuid.UUID, pagina int) ([]Visao, error) {
	if pagina < 1 || pagina > 1000 {
		pagina = 1
	}
	return repositorio{q: db.New(s.pool)}.doUsuario(ctx, usuarioID, TamanhoPaginaPedidos, int32((pagina-1)*TamanhoPaginaPedidos)) //nolint:gosec // página limitada acima
}

// Retomar devolve o segredo do cliente de um pedido ainda aguardando
// pagamento, direto do gateway (PRD 0031 — o segredo nunca é persistido). Só
// o carrinho dono; fora do prazo, sem cobrança ou em outro estado → não
// encontrado (o SPA volta ao mapa).
func (s *Servico) Retomar(ctx context.Context, id uuid.UUID, donoHash []byte) (Retomada, error) {
	linhas, err := db.New(s.pool).PendenteParaRetomar(ctx, db.PendenteParaRetomarParams{ID: id, DonoHash: donoHash})
	if err != nil {
		return Retomada{}, fmt.Errorf("pedido: retomar: %w", err)
	}
	if len(linhas) == 0 {
		return Retomada{}, ErrPedidoNaoEncontrado
	}
	p := linhas[0]
	if Status(p.Status) != AguardandoPagamento || !p.ExpiraEm.After(s.cfg.Agora()) || p.PaymentIntentID == nil {
		return Retomada{}, ErrPedidoNaoEncontrado
	}
	segredo, err := s.cfg.Gateway.RecuperarSegredo(ctx, *p.PaymentIntentID)
	if err != nil {
		s.logger.Warn("pedido: recuperar cobrança para retomada", zap.String("pedido_id", id.String()), zap.Error(err))
		return Retomada{}, ErrGatewayIndisponivel
	}
	return Retomada{Codigo: p.Codigo, TotalCentavos: p.TotalCentavos, ExpiraEm: p.ExpiraEm, SegredoCliente: segredo}, nil
}

// Retomada é o pagamento retomado: dados para o checkout + segredo do cliente.
type Retomada struct {
	Codigo         string
	TotalCentavos  int64
	ExpiraEm       time.Time
	SegredoCliente string
}
