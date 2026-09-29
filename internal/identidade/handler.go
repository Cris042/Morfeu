package identidade

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/autenticacao"
)

// limiteCorpo protege o grupo /auth de payloads gigantes (RF09).
const limiteCorpo = "16K"

// Cookie do refresh (PRD 0010 RF03): escopo restrito ao path do refresh —
// o logout fica sob o mesmo path (/auth/refresh/logout) para recebê-lo.
const (
	cookieRefresh     = "morfeu_refresh"
	pathCookieRefresh = "/auth/refresh"
	// headerAntiCSRF: só JS same-origin consegue setar (defesa em
	// profundidade além do SameSite=Strict — refinamento E1).
	headerAntiCSRF = "X-Requested-With"
	valorAntiCSRF  = "morfeu"
)

// Handler expõe as rotas /auth/* (RF02–RF04). Não há rota que crie ou
// promova operador (RF05).
type Handler struct {
	servico *Servico
	emissor *autenticacao.Emissor
	logger  *zap.Logger
}

// NovoHandler cria o handler HTTP do módulo.
func NovoHandler(s *Servico, e *autenticacao.Emissor, logger *zap.Logger) *Handler {
	return &Handler{servico: s, emissor: e, logger: logger}
}

// RegistrarRotas monta /auth/registro, /auth/login e /auth/eu no Echo.
func (h *Handler) RegistrarRotas(e *echo.Echo) {
	g := e.Group("/auth", middleware.BodyLimit(limiteCorpo))
	g.POST("/registro", h.registrar)
	g.POST("/login", h.login)
	g.GET("/eu", h.eu, autenticacao.Exigir(h.emissor, autenticacao.PapelCliente, autenticacao.PapelOperador))
	g.POST("/refresh", h.refresh, exigirAntiCSRF)
	g.POST("/refresh/logout", h.logout, exigirAntiCSRF)
	// Só cliente remove a própria conta pela API (operador → 403).
	g.DELETE("/conta", h.removerConta, autenticacao.Exigir(h.emissor, autenticacao.PapelCliente))
}

// exigirAntiCSRF barra rotas autenticadas por cookie sem o header custom.
func exigirAntiCSRF(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Header.Get(headerAntiCSRF) != valorAntiCSRF {
			return c.JSON(http.StatusForbidden, map[string]string{"erro": "csrf"})
		}
		return next(c)
	}
}

func (h *Handler) refresh(c echo.Context) error {
	token := ""
	if ck, err := c.Cookie(cookieRefresh); err == nil {
		token = ck.Value
	}
	sessao, err := h.servico.Renovar(c.Request().Context(), token)
	if err != nil {
		limparCookieRefresh(c)
		if errors.Is(err, ErrRefreshInvalido) {
			return c.JSON(http.StatusUnauthorized, map[string]string{"erro": "sessao_invalida"})
		}
		return h.responderErro(c, err)
	}
	return responderSessao(c, sessao)
}

func (h *Handler) logout(c echo.Context) error {
	token := ""
	if ck, err := c.Cookie(cookieRefresh); err == nil {
		token = ck.Value
	}
	if err := h.servico.Encerrar(c.Request().Context(), token); err != nil {
		return h.responderErro(c, err)
	}
	limparCookieRefresh(c)
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) removerConta(c echo.Context) error {
	id, ok := autenticacao.UsuarioID(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"erro": "nao_autenticado"})
	}
	if err := h.servico.RemoverConta(c.Request().Context(), id); err != nil {
		if errors.Is(err, ErrContaNaoRemovivel) {
			return c.JSON(http.StatusForbidden, map[string]string{"erro": "acesso_negado"})
		}
		return h.responderErro(c, err)
	}
	limparCookieRefresh(c)
	return c.NoContent(http.StatusNoContent)
}

// responderSessao: access no corpo, refresh no cookie HttpOnly.
func responderSessao(c echo.Context, s Sessao) error {
	c.SetCookie(&http.Cookie{
		Name:     cookieRefresh,
		Value:    s.Refresh.Token,
		Path:     pathCookieRefresh,
		MaxAge:   int(TTLRefresh.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusOK, sessaoDTO{
		AccessToken: s.AccessToken,
		TokenType:   "Bearer",
		ExpiraEm:    int64(time.Until(s.ExpiraEm).Seconds()),
	})
}

func limparCookieRefresh(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name: cookieRefresh, Value: "", Path: pathCookieRefresh, MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

// registroDTO não tem campo de papel: um "papel" no JSON é ignorado (RN01).
type registroDTO struct {
	Nome  string `json:"nome"`
	Email string `json:"email"`
	Senha string `json:"senha"`
}

type loginDTO struct {
	Email string `json:"email"`
	Senha string `json:"senha"`
}

type sessaoDTO struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiraEm    int64  `json:"expira_em"`
}

func (h *Handler) registrar(c echo.Context) error {
	var in registroDTO
	if err := json.NewDecoder(c.Request().Body).Decode(&in); err != nil {
		return requisicaoInvalida(c)
	}
	u, err := h.servico.Registrar(c.Request().Context(), EntradaRegistro(in), c.RealIP())
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusCreated, u)
}

func (h *Handler) login(c echo.Context) error {
	var in loginDTO
	if err := json.NewDecoder(c.Request().Body).Decode(&in); err != nil {
		return requisicaoInvalida(c)
	}
	sessao, err := h.servico.Login(c.Request().Context(), in.Email, in.Senha, c.RealIP())
	if err != nil {
		return h.responderErro(c, err)
	}
	return responderSessao(c, sessao)
}

func (h *Handler) eu(c echo.Context) error {
	id, ok := autenticacao.UsuarioID(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"erro": "nao_autenticado"})
	}
	u, err := h.servico.Eu(c.Request().Context(), id)
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, u)
}

// responderErro mapeia erros de domínio para respostas genéricas; detalhe
// técnico só no log (sem PII).
func (h *Handler) responderErro(c echo.Context, err error) error {
	var ev *ErroValidacao
	switch {
	case errors.As(err, &ev):
		return c.JSON(http.StatusBadRequest, map[string]any{"erro": "dados_invalidos", "campos": ev.Campos})
	case errors.Is(err, ErrEmailEmUso):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "email_em_uso"})
	case errors.Is(err, ErrCredenciaisInvalidas):
		return c.JSON(http.StatusUnauthorized, map[string]string{"erro": "credenciais_invalidas"})
	case errors.Is(err, ErrMuitasTentativas):
		return c.JSON(http.StatusTooManyRequests, map[string]string{"erro": "muitas_tentativas"})
	case errors.Is(err, ErrSaturado):
		return c.JSON(http.StatusTooManyRequests, map[string]string{"erro": "tente_novamente"})
	case errors.Is(err, ErrUsuarioNaoEncontrado):
		return c.JSON(http.StatusUnauthorized, map[string]string{"erro": "nao_autenticado"})
	default:
		h.logger.Error("identidade: erro inesperado", zap.Error(err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"erro": "erro_interno"})
	}
}

// requisicaoInvalida responde a JSON malformado sem ecoar nem logar o corpo
// (que pode conter a senha — CA08).
func requisicaoInvalida(c echo.Context) error {
	return c.JSON(http.StatusBadRequest, map[string]string{"erro": "requisicao_invalida"})
}
