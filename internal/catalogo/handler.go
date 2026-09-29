package catalogo

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

// limiteCorpoBackoffice protege as rotas de escrita do backoffice.
const limiteCorpoBackoffice = "32K"

// Handler expõe o catálogo por HTTP (RF02/RF03 do PRD 0011).
type Handler struct {
	servico *Servico
	logger  *zap.Logger
}

// NovoHandler cria o handler HTTP do catálogo.
func NovoHandler(s *Servico, logger *zap.Logger) *Handler {
	return &Handler{servico: s, logger: logger}
}

// RegistrarRotasPublicas monta o cartaz: GET /filmes e GET /filmes/:id.
func (h *Handler) RegistrarRotasPublicas(e *echo.Echo) {
	e.GET("/filmes", h.listarPublicos)
	e.GET("/filmes/:id", h.buscarPublico)
}

// RegistrarRotasBackoffice monta /backoffice/filmes protegido pelo middleware
// de papel recebido (autenticacao.Exigir(operador), montado no main — o
// catálogo não importa a plataforma de autenticação).
func (h *Handler) RegistrarRotasBackoffice(e *echo.Echo, exigirOperador echo.MiddlewareFunc) {
	g := e.Group("/backoffice/filmes", middleware.BodyLimit(limiteCorpoBackoffice), exigirOperador)
	g.GET("", h.listarBackoffice)
	g.POST("", h.criar)
	g.PUT("/:id", h.atualizar)
	g.POST("/:id/arquivar", h.arquivar)
}

// filmeDTO é a entrada do backoffice. Não aceita id/tmdb_id/arquivado_em.
type filmeDTO struct {
	Titulo     string  `json:"titulo"`
	Sinopse    *string `json:"sinopse"`
	DuracaoMin *int32  `json:"duracao_min"`
	Ano        *int32  `json:"ano"`
	PosterURL  *string `json:"poster_url"`
	ImdbID     *string `json:"imdb_id"`
}

func (h *Handler) listarPublicos(c echo.Context) error {
	filmes, err := h.servico.ListarPublicos(c.Request().Context())
	if err != nil {
		return h.responderErro(c, err)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=300")
	return c.JSON(http.StatusOK, filmes)
}

func (h *Handler) buscarPublico(c echo.Context) error {
	id, ok := idDaRota(c)
	if !ok {
		return naoEncontrado(c)
	}
	f, err := h.servico.BuscarPublico(c.Request().Context(), id)
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, f)
}

func (h *Handler) listarBackoffice(c echo.Context) error {
	filmes, err := h.servico.ListarBackoffice(c.Request().Context())
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, filmes)
}

func (h *Handler) criar(c echo.Context) error {
	var in filmeDTO
	if err := json.NewDecoder(c.Request().Body).Decode(&in); err != nil {
		return requisicaoInvalida(c)
	}
	f, err := h.servico.Criar(c.Request().Context(), DadosFilme(in))
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusCreated, f)
}

func (h *Handler) atualizar(c echo.Context) error {
	id, ok := idDaRota(c)
	if !ok {
		return naoEncontrado(c)
	}
	var in filmeDTO
	if err := json.NewDecoder(c.Request().Body).Decode(&in); err != nil {
		return requisicaoInvalida(c)
	}
	f, err := h.servico.Atualizar(c.Request().Context(), id, DadosFilme(in))
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, f)
}

func (h *Handler) arquivar(c echo.Context) error {
	id, ok := idDaRota(c)
	if !ok {
		return naoEncontrado(c)
	}
	if err := h.servico.Arquivar(c.Request().Context(), id); err != nil {
		return h.responderErro(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) responderErro(c echo.Context, err error) error {
	var ev *ErroValidacao
	switch {
	case errors.As(err, &ev):
		return c.JSON(http.StatusBadRequest, map[string]any{"erro": "dados_invalidos", "campos": ev.Campos})
	case errors.Is(err, ErrFilmeNaoEncontrado):
		return naoEncontrado(c)
	default:
		h.logger.Error("catalogo: erro inesperado", zap.Error(err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"erro": "erro_interno"})
	}
}

func idDaRota(c echo.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	return id, err == nil && id > 0
}

func naoEncontrado(c echo.Context) error {
	return c.JSON(http.StatusNotFound, map[string]string{"erro": "nao_encontrado"})
}

func requisicaoInvalida(c echo.Context) error {
	return c.JSON(http.StatusBadRequest, map[string]string{"erro": "requisicao_invalida"})
}
