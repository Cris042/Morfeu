package pedido

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

// O pedido usa o mesmo carrinho e a mesma proteção anti-CSRF da reserva
// (PRD 0015 RF03): cookie HttpOnly/SameSite=Strict + X-Requested-With.
const (
	cookieCarrinho = "morfeu_carrinho"
	headerAntiCSRF = "X-Requested-With"
	valorAntiCSRF  = "morfeu"
	limiteCorpo    = "4K"
	// limiteWebhook: eventos de PaymentIntent têm poucos KB (refinamento E6).
	limiteWebhook       = "64K"
	cabecalhoAssinatura = "Stripe-Signature"
)

// Handler expõe as rotas do pedido (RF01, RF06).
type Handler struct {
	servico *Servico
	logger  *zap.Logger
}

// NovoHandler cria o handler HTTP do módulo.
func NovoHandler(s *Servico, logger *zap.Logger) *Handler {
	return &Handler{servico: s, logger: logger}
}

// RegistrarRotas monta as rotas do checkout.
func (h *Handler) RegistrarRotas(e *echo.Echo) {
	e.POST("/pedidos", h.criar, middleware.BodyLimit(limiteCorpo), exigirAntiCSRF)
	e.GET("/pedidos/:id", h.obter)
	if h.servico.cfg.Webhook != nil {
		// Rota pública, fora do anti-CSRF: a autenticidade é a assinatura.
		e.POST("/webhooks/stripe", h.webhook, middleware.BodyLimit(limiteWebhook))
	}
}

// webhook recebe o evento do gateway (RF01–RF04): assinatura sobre o corpo
// BRUTO antes de qualquer parse; 2xx só depois do commit do pivô; falha de
// processamento → 5xx para o Stripe reenviar. Nunca loga corpo nem assinatura.
func (h *Handler) webhook(c echo.Context) error {
	ctx := c.Request().Context()
	if err := h.servico.ContarWebhook(ctx, c.RealIP()); err != nil {
		return h.responderErro(c, err)
	}
	corpo, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"erro": "requisicao_invalida"})
	}
	ev, err := h.servico.cfg.Webhook.Verificar(corpo, c.Request().Header.Get(cabecalhoAssinatura))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"erro": "assinatura_invalida"})
	}
	if err := h.servico.ProcessarEvento(ctx, ev); err != nil {
		h.logger.Error("pedido: falha ao processar webhook", zap.String("tipo", string(ev.Tipo)), zap.Error(err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"erro": "erro_interno"})
	}
	return c.NoContent(http.StatusOK)
}

func exigirAntiCSRF(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Header.Get(headerAntiCSRF) != valorAntiCSRF {
			return c.JSON(http.StatusForbidden, map[string]string{"erro": "csrf"})
		}
		return next(c)
	}
}

func (h *Handler) donoDaRequisicao(c echo.Context) ([]byte, bool) {
	ck, err := c.Cookie(cookieCarrinho)
	if err != nil {
		return nil, false
	}
	return h.servico.DonoDoToken(ck.Value)
}

// pedidoDTO é a visão pública do pedido para o dono (nunca o e-mail nem o
// hash do carrinho).
type pedidoDTO struct {
	ID            uuid.UUID `json:"id"`
	Codigo        string    `json:"codigo"`
	SessaoID      int64     `json:"sessao_id"`
	Assentos      []string  `json:"assentos"`
	TotalCentavos int64     `json:"total_centavos"`
	Status        Status    `json:"status"`
	ExpiraEm      time.Time `json:"expira_em"`
}

func (h *Handler) criar(c echo.Context) error {
	ctx := c.Request().Context()
	dono, ok := h.donoDaRequisicao(c)
	if !ok {
		// Sem carrinho não há holds: nada a comprar.
		return c.JSON(http.StatusConflict, map[string]string{"erro": "holds_invalidos"})
	}
	if err := h.servico.ContarRequisicao(ctx, c.RealIP(), dono); err != nil {
		return h.responderErro(c, err)
	}
	var in struct {
		Email    string   `json:"email"`
		SessaoID int64    `json:"sessao_id"`
		Assentos []string `json:"assentos"`
	}
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields() // total/preço do cliente é recusado, nunca usado (doc.md §14.2)
	if err := dec.Decode(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"erro": "requisicao_invalida"})
	}
	criado, err := h.servico.Criar(ctx, dono, Entrada{Email: in.Email, SessaoID: in.SessaoID, Assentos: in.Assentos})
	if err != nil {
		return h.responderErro(c, err)
	}
	p := criado.Pedido
	// O client_secret confirma pagamento: nunca em cache (auditoria 0023).
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusCreated, map[string]any{
		"pedido": pedidoDTO{ID: p.id, Codigo: p.codigo, SessaoID: p.sessaoID, Assentos: p.Assentos(),
			TotalCentavos: p.totalCentavos, Status: p.status, ExpiraEm: p.expiraEm.UTC()},
		"client_secret": criado.SegredoCliente,
	})
}

func (h *Handler) obter(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	dono, ok := h.donoDaRequisicao(c)
	if err != nil || !ok {
		return naoEncontrado(c)
	}
	v, err := h.servico.Obter(c.Request().Context(), id, dono)
	if err != nil {
		return h.responderErro(c, err)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusOK, pedidoDTO{ID: v.ID, Codigo: v.Codigo, SessaoID: v.SessaoID, Assentos: v.Assentos,
		TotalCentavos: v.TotalCentavos, Status: v.Status, ExpiraEm: v.ExpiraEm.UTC()})
}

func (h *Handler) responderErro(c echo.Context, err error) error {
	var ev *ErroValidacao
	var ep *ErroPendente
	switch {
	case errors.As(err, &ev):
		return c.JSON(http.StatusBadRequest, map[string]any{"erro": "dados_invalidos", "campos": ev.Campos})
	case errors.As(err, &ep):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "pedido_pendente", "pedido_id": ep.PedidoID})
	case errors.Is(err, ErrPedidoPendente):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "pedido_pendente"})
	case errors.Is(err, ErrHoldsInvalidos):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "holds_invalidos"})
	case errors.Is(err, ErrMuitasRequisicoes):
		return c.JSON(http.StatusTooManyRequests, map[string]string{"erro": "muitas_requisicoes"})
	case errors.Is(err, ErrGatewayIndisponivel):
		c.Response().Header().Set("Retry-After", "30")
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"erro": "pagamento_indisponivel"})
	case errors.Is(err, ErrSessaoIndisponivel), errors.Is(err, ErrPedidoNaoEncontrado):
		return naoEncontrado(c)
	default:
		h.logger.Error("pedido: erro inesperado", zap.Error(err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"erro": "erro_interno"})
	}
}

func naoEncontrado(c echo.Context) error {
	return c.JSON(http.StatusNotFound, map[string]string{"erro": "nao_encontrado"})
}
