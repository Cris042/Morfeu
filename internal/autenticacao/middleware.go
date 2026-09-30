package autenticacao

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Chaves do contexto Echo populadas por Exigir (RF05).
const (
	chaveUsuarioID = "autenticacao.usuario_id"
	chavePapel     = "autenticacao.papel"
)

// Respostas genéricas — nunca revelam a causa (expirado, assinatura, kid...).
var (
	respNaoAutenticado = map[string]string{"erro": "nao_autenticado"}
	respAcessoNegado   = map[string]string{"erro": "acesso_negado"}
)

// Exigir protege a rota: exige access token válido e um dos papéis listados
// (RF04). Fail-closed: sem papéis na lista, a rota nega sempre — esquecer de
// declarar o papel nunca deixa a rota aberta.
func Exigir(e *Emissor, papeis ...Papel) echo.MiddlewareFunc {
	permitidos := make(map[Papel]bool, len(papeis))
	for _, p := range papeis {
		permitidos[p] = true
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token, ok := bearer(c.Request().Header.Get(echo.HeaderAuthorization))
			if !ok {
				return naoAutenticado(c)
			}
			claims, err := e.Validar(token)
			if err != nil {
				return naoAutenticado(c)
			}
			if !permitidos[claims.Papel] {
				return c.JSON(http.StatusForbidden, respAcessoNegado)
			}
			c.Set(chaveUsuarioID, claims.UsuarioIDDe())
			c.Set(chavePapel, claims.Papel)
			return next(c)
		}
	}
}

// Opcional autentica quando há token (PRD 0031 — checkout de convidado ou
// logado): sem header Authorization segue anônimo; header presente e
// inválido → 401 (nunca cai para anônimo em silêncio — o SPA renova e
// repete); válido → usuário e papel no contexto, como em Exigir.
func Opcional(e *Emissor) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get(echo.HeaderAuthorization)
			if header == "" {
				return next(c)
			}
			token, ok := bearer(header)
			if !ok {
				return naoAutenticado(c)
			}
			claims, err := e.Validar(token)
			if err != nil {
				return naoAutenticado(c)
			}
			c.Set(chaveUsuarioID, claims.UsuarioIDDe())
			c.Set(chavePapel, claims.Papel)
			return next(c)
		}
	}
}

func naoAutenticado(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderWWWAuthenticate, "Bearer")
	return c.JSON(http.StatusUnauthorized, respNaoAutenticado)
}

// bearer extrai o token de "Bearer <token>" (esquema case-insensitive).
func bearer(header string) (string, bool) {
	esquema, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(esquema, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// UsuarioID devolve o usuário autenticado da requisição (rotas com Exigir,
// ou com Opcional quando havia token).
func UsuarioID(c echo.Context) (uuid.UUID, bool) {
	id, ok := c.Get(chaveUsuarioID).(uuid.UUID)
	return id, ok
}

// PapelDe devolve o papel do usuário autenticado da requisição.
func PapelDe(c echo.Context) (Papel, bool) {
	p, ok := c.Get(chavePapel).(Papel)
	return p, ok
}
