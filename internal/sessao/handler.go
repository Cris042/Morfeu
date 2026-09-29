package sessao

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

// limiteCorpo: o layout (≤ 64 KB) mais o envelope da requisição.
const limiteCorpo = "80K"

// Handler expõe o backoffice de salas e sessões (RF03–RF05 do PRD 0013).
type Handler struct {
	servico *Servico
	logger  *zap.Logger
}

// NovoHandler cria o handler HTTP do módulo.
func NovoHandler(s *Servico, logger *zap.Logger) *Handler {
	return &Handler{servico: s, logger: logger}
}

// RegistrarRotasBackoffice monta /backoffice/salas e /backoffice/sessoes com
// o middleware de papel e a função que identifica o operador — ambos vindos
// do main (o módulo não importa a plataforma de autenticação).
func (h *Handler) RegistrarRotasBackoffice(e *echo.Echo, exigirOperador echo.MiddlewareFunc, operadorDe func(echo.Context) string) {
	salas := e.Group("/backoffice/salas", middleware.BodyLimit(limiteCorpo), exigirOperador)
	salas.POST("", func(c echo.Context) error { return h.criarSala(c, operadorDe(c)) })
	salas.GET("", h.listarSalas)
	salas.PUT("/:id", func(c echo.Context) error { return h.atualizarSala(c, operadorDe(c)) })

	sessoes := e.Group("/backoffice/sessoes", middleware.BodyLimit(limiteCorpo), exigirOperador)
	sessoes.POST("", func(c echo.Context) error { return h.criarSessao(c, operadorDe(c)) })
	sessoes.GET("", h.listarSessoes)
	sessoes.POST("/:id/cancelar", func(c echo.Context) error { return h.cancelarSessao(c, operadorDe(c)) })
}

type salaDTO struct {
	Nome   string          `json:"nome"`
	Layout json.RawMessage `json:"layout"`
}

type sessaoDTO struct {
	FilmeID       int64  `json:"filme_id"`
	SalaID        int64  `json:"sala_id"`
	Inicio        string `json:"inicio"`
	PrecoCentavos int32  `json:"preco_centavos"`
}

func decodificarEstrito(c echo.Context, destino any) error {
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	return dec.Decode(destino)
}

func (h *Handler) criarSala(c echo.Context, operador string) error {
	var in salaDTO
	if err := decodificarEstrito(c, &in); err != nil {
		return requisicaoInvalida(c)
	}
	sala, err := h.servico.CriarSala(c.Request().Context(), in.Nome, bytes.TrimSpace(in.Layout), operador)
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusCreated, sala)
}

func (h *Handler) atualizarSala(c echo.Context, operador string) error {
	id, ok := idDaRota(c)
	if !ok {
		return naoEncontrado(c)
	}
	var in salaDTO
	if err := decodificarEstrito(c, &in); err != nil {
		return requisicaoInvalida(c)
	}
	sala, err := h.servico.AtualizarSala(c.Request().Context(), id, in.Nome, bytes.TrimSpace(in.Layout), operador)
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, sala)
}

func (h *Handler) listarSalas(c echo.Context) error {
	salas, err := h.servico.ListarSalas(c.Request().Context())
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, salas)
}

func (h *Handler) criarSessao(c echo.Context, operador string) error {
	var in sessaoDTO
	if err := decodificarEstrito(c, &in); err != nil {
		return requisicaoInvalida(c)
	}
	inicio, err := time.Parse(time.RFC3339, in.Inicio)
	if err != nil {
		return h.responderErro(c, &ErroValidacao{Campos: []string{"inicio"}})
	}
	s, err := h.servico.CriarSessao(c.Request().Context(), EntradaSessao{
		FilmeID: in.FilmeID, SalaID: in.SalaID, Inicio: inicio, PrecoCentavos: in.PrecoCentavos,
	}, operador)
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusCreated, s)
}

func (h *Handler) listarSessoes(c echo.Context) error {
	sessoes, err := h.servico.ListarSessoesBackoffice(c.Request().Context())
	if err != nil {
		return h.responderErro(c, err)
	}
	return c.JSON(http.StatusOK, sessoes)
}

func (h *Handler) cancelarSessao(c echo.Context, operador string) error {
	id, ok := idDaRota(c)
	if !ok {
		return naoEncontrado(c)
	}
	if err := h.servico.CancelarSessao(c.Request().Context(), id, operador); err != nil {
		return h.responderErro(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) responderErro(c echo.Context, err error) error {
	var ev *ErroValidacao
	var ec *ErroConflitoHorario
	switch {
	case errors.As(err, &ev):
		return c.JSON(http.StatusBadRequest, map[string]any{"erro": "dados_invalidos", "campos": ev.Campos})
	case errors.As(err, &ec):
		corpo := map[string]any{"erro": "conflito_horario"}
		if ec.SessaoID != 0 {
			corpo["conflitante"] = map[string]any{"sessao_id": ec.SessaoID, "inicio": ec.Inicio.UTC(), "fim": ec.Fim.UTC()}
		}
		return c.JSON(http.StatusConflict, corpo)
	case errors.Is(err, ErrNomeSalaEmUso):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "nome_em_uso"})
	case errors.Is(err, ErrLayoutEmUso):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "layout_em_uso"})
	case errors.Is(err, ErrSalaInexistente):
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"erro": "sala_inexistente"})
	case errors.Is(err, ErrFilmeIndisponivel):
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"erro": "filme_indisponivel"})
	case errors.Is(err, ErrSalaNaoEncontrada), errors.Is(err, ErrSessaoNaoEncontrada):
		return naoEncontrado(c)
	default:
		h.logger.Error("sessao: erro inesperado", zap.Error(err))
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
