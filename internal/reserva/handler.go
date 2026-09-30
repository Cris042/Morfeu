package reserva

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

// Cookie do carrinho (RF03; decisão do usuário: cookie HttpOnly + anti-CSRF,
// o mesmo mecanismo do refresh do E1).
const (
	cookieCarrinho = "morfeu_carrinho"
	headerAntiCSRF = "X-Requested-With"
	valorAntiCSRF  = "morfeu"
	limiteCorpo    = "4K"
)

// Handler expõe as rotas de trava (RF04–RF07).
type Handler struct {
	servico *Servico
	logger  *zap.Logger
}

// NovoHandler cria o handler HTTP do módulo.
func NovoHandler(s *Servico, logger *zap.Logger) *Handler {
	return &Handler{servico: s, logger: logger}
}

// RegistrarRotas monta as rotas públicas da trava e da ocupação. As que mudam estado
// exigem o header anti-CSRF (só JS same-origin consegue enviá-lo).
func (h *Handler) RegistrarRotas(e *echo.Echo) {
	e.POST("/sessoes/:id/holds", h.travar, middleware.BodyLimit(limiteCorpo), exigirAntiCSRF)
	e.GET("/sessoes/:id/ocupacao", h.ocupacao)
	e.GET("/holds", h.meusHolds)
	e.POST("/holds/:id/estender", h.estender, exigirAntiCSRF)
	e.DELETE("/holds/:id", h.liberar, exigirAntiCSRF)
}

func exigirAntiCSRF(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Header.Get(headerAntiCSRF) != valorAntiCSRF {
			return c.JSON(http.StatusForbidden, map[string]string{"erro": "csrf"})
		}
		return next(c)
	}
}

// holdDTO é a visão pública do hold: nada sobre o dono.
type holdDTO struct {
	ID              uuid.UUID `json:"id"`
	SessaoID        int64     `json:"sessao_id"`
	Assento         string    `json:"assento"`
	ExpiraEm        time.Time `json:"expira_em"`
	ExtensoesUsadas int       `json:"extensoes_usadas"`
}

func paraDTO(h Hold) holdDTO {
	return holdDTO{ID: h.id, SessaoID: h.sessaoID, Assento: string(h.assento), ExpiraEm: h.expiraEm.UTC(), ExtensoesUsadas: h.extensoesUsadas}
}

func paraDTOs(hs []Hold) []holdDTO {
	out := make([]holdDTO, 0, len(hs))
	for _, h := range hs {
		out = append(out, paraDTO(h))
	}
	return out
}

// donoDaRequisicao lê o carrinho do cookie; ok=false se ausente ou inválido.
func donoDaRequisicao(c echo.Context) (Dono, bool) {
	ck, err := c.Cookie(cookieCarrinho)
	if err != nil {
		return Dono{}, false
	}
	return DonoDoToken(ck.Value)
}

func (h *Handler) travar(c echo.Context) error {
	sessaoID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessaoID <= 0 {
		return naoEncontrado(c)
	}
	ctx := c.Request().Context()
	dono, temDono := donoDaRequisicao(c)
	if err := h.servico.ContarRequisicao(ctx, c.RealIP(), dono, temDono); err != nil {
		return h.responderErro(c, err)
	}
	var in struct {
		Assentos []string `json:"assentos"`
	}
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"erro": "requisicao_invalida"})
	}
	token := ""
	if !temDono {
		if token, dono, err = NovoCarrinho(); err != nil {
			return h.responderErro(c, err)
		}
	}
	holds, err := h.servico.Travar(ctx, sessaoID, in.Assentos, dono)
	if err != nil {
		return h.responderErro(c, err)
	}
	if token != "" {
		// Só emitido quando a trava deu certo: nenhum carrinho órfão no navegador.
		c.SetCookie(&http.Cookie{
			Name: cookieCarrinho, Value: token, Path: "/",
			HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
		})
	}
	return c.JSON(http.StatusCreated, map[string]any{"holds": paraDTOs(holds)})
}

// ocupacao: leitura pública para o polling do mapa (PRD 0016).
func (h *Handler) ocupacao(c echo.Context) error {
	sessaoID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessaoID <= 0 {
		return naoEncontrado(c)
	}
	o, err := h.servico.Ocupacao(c.Request().Context(), sessaoID)
	if err != nil {
		return h.responderErro(c, err)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=2")
	return c.JSON(http.StatusOK, o)
}

func (h *Handler) meusHolds(c echo.Context) error {
	dono, ok := donoDaRequisicao(c)
	if !ok {
		return c.JSON(http.StatusOK, []holdDTO{})
	}
	holds, err := h.servico.MeusHolds(c.Request().Context(), dono)
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, paraDTOs(holds))
}

func (h *Handler) estender(c echo.Context) error {
	id, dono, ok := holdDaRota(c)
	if !ok {
		return naoEncontrado(c)
	}
	ctx := c.Request().Context()
	if err := h.servico.ContarRequisicao(ctx, c.RealIP(), dono, true); err != nil {
		return h.responderErro(c, err)
	}
	hold, err := h.servico.Estender(ctx, id, dono)
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, paraDTO(hold))
}

func (h *Handler) liberar(c echo.Context) error {
	id, dono, ok := holdDaRota(c)
	if !ok {
		return naoEncontrado(c)
	}
	if err := h.servico.Liberar(c.Request().Context(), id, dono); err != nil {
		return h.responderErro(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// holdDaRota: id malformado ou sem carrinho = 404 (nunca 403 — RN05).
func holdDaRota(c echo.Context) (uuid.UUID, Dono, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.UUID{}, Dono{}, false
	}
	dono, ok := donoDaRequisicao(c)
	return id, dono, ok
}

func (h *Handler) responderErro(c echo.Context, err error) error {
	var ev *ErroValidacao
	var ei *ErroIndisponivel
	switch {
	case errors.As(err, &ev):
		return c.JSON(http.StatusBadRequest, map[string]any{"erro": "dados_invalidos", "campos": ev.Campos})
	case errors.As(err, &ei):
		return c.JSON(http.StatusConflict, map[string]any{"erro": "assento_indisponivel", "assentos": ei.Assentos})
	case errors.Is(err, ErrLimiteHolds):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "limite_holds"})
	case errors.Is(err, ErrExtensaoEsgotada):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "extensao_esgotada"})
	case errors.Is(err, ErrHoldEmPedido):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "hold_em_pedido"})
	case errors.Is(err, ErrMuitasRequisicoes):
		return c.JSON(http.StatusTooManyRequests, map[string]string{"erro": "muitas_requisicoes"})
	case errors.Is(err, ErrSessaoIndisponivel), errors.Is(err, ErrHoldNaoEncontrado):
		return naoEncontrado(c)
	default:
		h.logger.Error("reserva: erro inesperado", zap.Error(err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"erro": "erro_interno"})
	}
}

func naoEncontrado(c echo.Context) error {
	return c.JSON(http.StatusNotFound, map[string]string{"erro": "nao_encontrado"})
}
