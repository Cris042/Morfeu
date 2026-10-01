package pedido

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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
	// Conta (PRD 0031): middlewares e leitura do usuário injetados pelo main
	// (o pedido não importa autenticacao — ADR 0003).
	opcional     echo.MiddlewareFunc
	exigirConta  echo.MiddlewareFunc
	usuarioDe    func(echo.Context) (uuid.UUID, bool)
	rotasDeTeste bool
}

// ComConta liga a conta: Bearer opcional no checkout e na leitura do pedido,
// obrigatório em "Meus pedidos".
func (h *Handler) ComConta(opcional, exigir echo.MiddlewareFunc, usuarioDe func(echo.Context) (uuid.UUID, bool)) *Handler {
	h.opcional, h.exigirConta, h.usuarioDe = opcional, exigir, usuarioDe
	return h
}

// ComRotasDeTeste registra POST /__teste/pagar/:id — SÓ com o gateway fake
// (o main decide; o boot recusa fake em produção).
func (h *Handler) ComRotasDeTeste() *Handler {
	h.rotasDeTeste = true
	return h
}

// usuario devolve a conta autenticada da requisição (nil = convidado).
func (h *Handler) usuario(c echo.Context) *uuid.UUID {
	if h.usuarioDe == nil {
		return nil
	}
	if id, ok := h.usuarioDe(c); ok {
		return &id
	}
	return nil
}

func semMiddleware(next echo.HandlerFunc) echo.HandlerFunc { return next }

// NovoHandler cria o handler HTTP do módulo.
func NovoHandler(s *Servico, logger *zap.Logger) *Handler {
	return &Handler{servico: s, logger: logger}
}

// RegistrarRotas monta as rotas do checkout.
func (h *Handler) RegistrarRotas(e *echo.Echo) {
	opcional := h.opcional
	if opcional == nil {
		opcional = semMiddleware
	}
	e.POST("/pedidos", h.criar, middleware.BodyLimit(limiteCorpo), exigirAntiCSRF, opcional)
	e.GET("/pedidos/:id", h.obter, opcional)
	e.POST("/pedidos/:id/retomar", h.retomar, exigirAntiCSRF)
	if h.exigirConta != nil {
		e.GET("/pedidos", h.meusPedidos, h.exigirConta)
		e.POST("/pedidos/:id/cancelar", h.cancelarDaConta, exigirAntiCSRF, h.exigirConta)
	}
	if h.servico.cfg.Consulta != nil {
		e.POST("/pedidos/consulta", h.consultar, middleware.BodyLimit(limiteCorpo), exigirAntiCSRF)
		e.POST("/pedidos/consulta/cancelar", h.cancelarPorConsulta, middleware.BodyLimit(limiteCorpo), exigirAntiCSRF)
		e.GET("/i/:ref", h.ingresso, cabecalhosIngresso)
		e.GET("/i/:ref/qr.png", h.qrIngresso, cabecalhosIngresso)
	}
	if h.rotasDeTeste {
		e.POST("/__teste/pagar/:id", h.pagarParaTeste)
	}
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
	Cancelavel    bool      `json:"cancelavel"`
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
	criado, err := h.servico.Criar(ctx, dono, Entrada{Email: in.Email, SessaoID: in.SessaoID, Assentos: in.Assentos, UsuarioID: h.usuario(c)})
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
	dono, temCarrinho := h.donoDaRequisicao(c)
	usuario := h.usuario(c)
	if err != nil || (!temCarrinho && usuario == nil) {
		return naoEncontrado(c)
	}
	v, err := h.servico.Obter(c.Request().Context(), id, dono, usuario)
	if err != nil {
		return h.responderErro(c, err)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusOK, paraPedidoDTO(v))
}

func paraPedidoDTO(v Visao) pedidoDTO {
	return pedidoDTO{ID: v.ID, Codigo: v.Codigo, SessaoID: v.SessaoID, Assentos: v.Assentos,
		TotalCentavos: v.TotalCentavos, Status: v.Status, ExpiraEm: v.ExpiraEm.UTC(), Cancelavel: v.Cancelavel}
}

// meusPedidos: "Meus pedidos" da conta logada (PRD 0031), ?pagina=N.
func (h *Handler) meusPedidos(c echo.Context) error {
	usuario := h.usuario(c)
	if usuario == nil {
		return naoEncontrado(c)
	}
	pagina, _ := strconv.Atoi(c.QueryParam("pagina"))
	lista, err := h.servico.MeusPedidos(c.Request().Context(), *usuario, pagina)
	if err != nil {
		return h.responderErro(c, err)
	}
	out := make([]pedidoDTO, 0, len(lista))
	for _, v := range lista {
		out = append(out, paraPedidoDTO(v))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusOK, map[string]any{"pedidos": out})
}

// retomar devolve o client_secret de um pedido pendente ao carrinho dono
// (PRD 0031) — lido do gateway, nunca guardado; resposta sem cache.
func (h *Handler) retomar(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	dono, ok := h.donoDaRequisicao(c)
	if err != nil || !ok {
		return naoEncontrado(c)
	}
	r, err := h.servico.Retomar(c.Request().Context(), id, dono)
	if err != nil {
		return h.responderErro(c, err)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusOK, map[string]any{
		"pedido_id": id, "codigo": r.Codigo, "total_centavos": r.TotalCentavos,
		"expira_em": r.ExpiraEm.UTC(), "client_secret": r.SegredoCliente,
	})
}

// cancelarDaConta: o cliente logado cancela o próprio pedido (PRD 0036 RF05).
func (h *Handler) cancelarDaConta(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	usuario := h.usuario(c)
	if err != nil || usuario == nil {
		return naoEncontrado(c)
	}
	v, err := h.servico.CancelarDaConta(c.Request().Context(), id, *usuario)
	if err != nil {
		return h.responderErro(c, err)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusOK, paraPedidoDTO(v))
}

// consultar: pedido de convidado por e-mail + código (PRD 0034). Todo "não
// encontrado" (código inexistente, malformado ou e-mail errado) sai idêntico.
func (h *Handler) consultar(c echo.Context) error {
	return h.peloConvidado(c, h.servico.Consultar)
}

// cancelarPorConsulta: o convidado cancela com e-mail + código (PRD 0036
// RF06) — mesmo limite, mesmo "não encontrado" e mesmo formato da consulta.
func (h *Handler) cancelarPorConsulta(c echo.Context) error {
	return h.peloConvidado(c, h.servico.CancelarPorConsulta)
}

func (h *Handler) peloConvidado(c echo.Context, acao func(ctx context.Context, email, codigo string) (ResultadoConsulta, error)) error {
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	var in struct {
		Email  string `json:"email"`
		Codigo string `json:"codigo"`
	}
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"erro": "requisicao_invalida"})
	}
	ctx := c.Request().Context()
	if err := h.servico.ContarConsulta(ctx, c.RealIP(), in.Email); err != nil {
		return h.responderErro(c, err)
	}
	r, err := acao(ctx, in.Email, in.Codigo)
	if err != nil {
		return h.responderErro(c, err)
	}
	ingressos := make([]map[string]string, 0, len(r.Ingressos))
	for _, i := range r.Ingressos {
		ingressos = append(ingressos, map[string]string{"assento": i.Assento, "ref": i.Ref})
	}
	return c.JSON(http.StatusOK, map[string]any{"pedido": paraPedidoDTO(r.Pedido), "ingressos": ingressos})
}

// cabecalhosIngresso: a página e o QR do ingresso nunca vão para cache nem
// vazam o link por Referer (refinamento E7/E8) — inclusive no 404/410.
func cabecalhosIngresso(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		h := c.Response().Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set(echo.HeaderCacheControl, "no-store")
		h.Set(echo.HeaderXContentTypeOptions, "nosniff")
		return next(c)
	}
}

// ingresso: GET idempotente (scanners de e-mail abrem o link) — só lê.
func (h *Handler) ingresso(c echo.Context) error {
	ctx := c.Request().Context()
	if err := h.servico.ContarIngresso(ctx, c.RealIP()); err != nil {
		return h.responderErro(c, err)
	}
	i, err := h.servico.Ingresso(ctx, c.Param("ref"))
	if err != nil {
		return h.responderErroIngresso(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"assento": i.Assento, "status": i.Status, "filme": i.Filme, "sala": i.Sala, "inicio": i.Inicio.UTC(),
	})
}

func (h *Handler) qrIngresso(c echo.Context) error {
	ctx := c.Request().Context()
	if err := h.servico.ContarIngresso(ctx, c.RealIP()); err != nil {
		return h.responderErro(c, err)
	}
	png, err := h.servico.QRDoIngresso(ctx, c.Param("ref"))
	if err != nil {
		return h.responderErroIngresso(c, err)
	}
	return c.Blob(http.StatusOK, "image/png", png)
}

func (h *Handler) responderErroIngresso(c echo.Context, err error) error {
	switch {
	case errors.Is(err, ErrIngressoInvalido):
		return naoEncontrado(c)
	case errors.Is(err, ErrIngressoIndisponivel):
		return c.JSON(http.StatusGone, map[string]string{"erro": "ingresso_indisponivel"})
	default:
		// Nunca o link no log: só o erro (sem ref/token).
		return h.responderErro(c, err)
	}
}

// pagarParaTeste: só registrada com gateway fake (ver ComRotasDeTeste).
func (h *Handler) pagarParaTeste(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return naoEncontrado(c)
	}
	if err := h.servico.PagarParaTeste(c.Request().Context(), id); err != nil {
		return h.responderErro(c, err)
	}
	return c.NoContent(http.StatusNoContent)
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
	case errors.Is(err, ErrNaoCancelavel):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "nao_cancelavel"})
	case errors.Is(err, ErrForaDaJanela):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "fora_da_janela"})
	case errors.Is(err, ErrSessaoJaComecou):
		return c.JSON(http.StatusConflict, map[string]string{"erro": "sessao_iniciada"})
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

// RegistrarRotasBackoffice monta a consulta e o cancelamento de pedidos do
// operador (PRD 0037); exigirOperador vem do main (RBAC) já com o ator da
// trilha de auditoria no context.
func (h *Handler) RegistrarRotasBackoffice(e *echo.Echo, exigirOperador echo.MiddlewareFunc) {
	g := e.Group("/backoffice/pedidos", exigirOperador)
	g.GET("", h.listarParaOperador)
	g.GET("/:id", h.detalharParaOperador)
	g.POST("/:id/cancelar", h.cancelarPeloOperador)
}

type pedidoOperadorDTO struct {
	ID            uuid.UUID `json:"id"`
	SessaoID      int64     `json:"sessao_id"`
	Email         string    `json:"email"` // sempre mascarado
	Assentos      []string  `json:"assentos"`
	TotalCentavos int64     `json:"total_centavos"`
	Status        Status    `json:"status"`
	MotivoEstorno string    `json:"motivo_estorno,omitempty"`
	CriadoEm      time.Time `json:"criado_em"`
}

func paraPedidoOperadorDTO(p PedidoOperador) pedidoOperadorDTO {
	return pedidoOperadorDTO{ID: p.ID, SessaoID: p.SessaoID, Email: p.EmailMascarado, Assentos: p.Assentos,
		TotalCentavos: p.TotalCentavos, Status: p.Status, MotivoEstorno: p.MotivoEstorno, CriadoEm: p.CriadoEm.UTC()}
}

// listarParaOperador: ?sessao_id=N&status=S&pagina=P (filtros validados).
func (h *Handler) listarParaOperador(c echo.Context) error {
	var f FiltroOperador
	if v := c.QueryParam("sessao_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return c.JSON(http.StatusBadRequest, map[string]any{"erro": "dados_invalidos", "campos": []string{"sessao_id"}})
		}
		f.SessaoID = &id
	}
	if v := c.QueryParam("status"); v != "" {
		st := Status(v)
		if !st.Valido() {
			return c.JSON(http.StatusBadRequest, map[string]any{"erro": "dados_invalidos", "campos": []string{"status"}})
		}
		f.Status = &st
	}
	pagina, _ := strconv.Atoi(c.QueryParam("pagina"))
	lista, err := h.servico.ListarParaOperador(c.Request().Context(), f, pagina)
	if err != nil {
		return h.responderErro(c, err)
	}
	out := make([]pedidoOperadorDTO, 0, len(lista))
	for _, p := range lista {
		out = append(out, paraPedidoOperadorDTO(p))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusOK, map[string]any{"pedidos": out})
}

func (h *Handler) detalharParaOperador(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return naoEncontrado(c)
	}
	d, err := h.servico.DetalharParaOperador(c.Request().Context(), id)
	if err != nil {
		return h.responderErro(c, err)
	}
	return h.responderDetalhe(c, d)
}

func (h *Handler) cancelarPeloOperador(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return naoEncontrado(c)
	}
	d, err := h.servico.CancelarPeloOperador(c.Request().Context(), id)
	if err != nil {
		return h.responderErro(c, err)
	}
	return h.responderDetalhe(c, d)
}

func (h *Handler) responderDetalhe(c echo.Context, d DetalheOperador) error {
	ingressos := make([]map[string]string, 0, len(d.Ingressos))
	for _, i := range d.Ingressos {
		ingressos = append(ingressos, map[string]string{"assento": i.Assento, "status": i.Status})
	}
	eventos := make([]map[string]any, 0, len(d.Eventos))
	for _, e := range d.Eventos {
		eventos = append(eventos, map[string]any{"de": e.De, "para": e.Para, "ocorrido_em": e.OcorridoEm.UTC()})
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return c.JSON(http.StatusOK, map[string]any{"pedido": paraPedidoOperadorDTO(d.PedidoOperador), "ingressos": ingressos, "eventos": eventos})
}
