package pedido

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mclovin137/morfeu/internal/pedido/db"
)

// Consulta de convidado e página pública do ingresso (PRD 0034).

var (
	// ErrIngressoInvalido: link malformado, ingresso inexistente ou token
	// errado — a mesma resposta para todos (sem oráculo).
	ErrIngressoInvalido = errors.New("pedido: ingresso inválido")
	// ErrIngressoIndisponivel: token válido, mas o ingresso expirou (início da
	// sessão + 24 h), foi cancelado ou o pedido não está mais pago. Só é
	// devolvido depois do HMAC conferido.
	ErrIngressoIndisponivel = errors.New("pedido: ingresso expirado ou cancelado")
)

// ValidadeIngresso: o link do ingresso vale até 24 h depois do início.
const ValidadeIngresso = 24 * time.Hour

const tamanhoToken = 43 // base64url sem padding de 32 bytes

// emailFantasma é comparado quando o código não existe: o caminho (hash +
// comparação em tempo constante) é o mesmo do "e-mail errado".
const emailFantasma = "nao-existe@morfeu.invalid"

// InfoSessao é o que a página do ingresso mostra da sessão (porta composta
// no main a partir de sessao + catalogo, ADR 0003).
type InfoSessao struct {
	Filme  string
	Sala   string
	Inicio time.Time
}

// ConfigConsulta liga a consulta de convidado e a página do ingresso; nil
// no Config = rotas desligadas.
type ConfigConsulta struct {
	LimiteIP       Limitador // consulta: por IP
	LimiteEmail    Limitador // consulta: por e-mail normalizado
	LimiteIngresso Limitador // página e QR do ingresso: por IP
	Sessao         func(ctx context.Context, sessaoID int64) (InfoSessao, error)
	// QR gera o PNG do conteúdo (o gerador da notificação, injetado pelo main).
	QR func(conteudo string) ([]byte, error)
	// BaseURL é a origem pública dos links (o QR repete o link do e-mail).
	BaseURL string
}

func (c *ConfigConsulta) validar() error {
	if c.LimiteIP == nil || c.LimiteEmail == nil || c.LimiteIngresso == nil || c.Sessao == nil || c.QR == nil || c.BaseURL == "" {
		return errors.New("pedido: consulta sem limitador, porta da sessão, QR ou URL base")
	}
	return nil
}

// IngressoDaConsulta é o link de um ingresso ativo: ref = "{id}.{token}".
type IngressoDaConsulta struct {
	Assento string
	Ref     string
}

// ResultadoConsulta é o pedido do convidado com os links dos ingressos.
type ResultadoConsulta struct {
	Pedido    Visao
	Ingressos []IngressoDaConsulta
}

// IngressoPublico é o que a página do ingresso mostra.
type IngressoPublico struct {
	Assento string
	Status  string // ativo | usado
	InfoSessao
}

func normalizarEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// normalizarCodigo aceita o código como o cliente o digita (minúsculas,
// espaços ou hífens de agrupamento).
func normalizarCodigo(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	return strings.NewReplacer(" ", "", "-", "").Replace(c)
}

func codigoValido(c string) bool {
	if len(c) != 16 {
		return false
	}
	for _, r := range c {
		if (r < 'A' || r > 'Z') && (r < '2' || r > '7') {
			return false
		}
	}
	return true
}

// ContarConsulta aplica o rate limit da consulta por IP e por e-mail.
func (s *Servico) ContarConsulta(ctx context.Context, ip, email string) error {
	c := s.cfg.Consulta
	chave := fmt.Sprintf("%x", sha256.Sum256([]byte(normalizarEmail(email)))) // e-mail nunca em claro no Redis
	if c.LimiteIP.Bloqueado(ctx, ip) || c.LimiteEmail.Bloqueado(ctx, chave) {
		return ErrMuitasRequisicoes
	}
	c.LimiteIP.RegistrarFalha(ctx, ip)
	c.LimiteEmail.RegistrarFalha(ctx, chave)
	return nil
}

// ContarIngresso aplica o rate limit por IP da página e do QR do ingresso.
func (s *Servico) ContarIngresso(ctx context.Context, ip string) error {
	l := s.cfg.Consulta.LimiteIngresso
	if l.Bloqueado(ctx, ip) {
		return ErrMuitasRequisicoes
	}
	l.RegistrarFalha(ctx, ip)
	return nil
}

// Consultar devolve o pedido de convidado por e-mail + código. Código
// inexistente e e-mail errado passam pelo mesmo caminho e dão o mesmo
// ErrPedidoNaoEncontrado. Links só de ingressos ativos de pedido pago.
func (s *Servico) Consultar(ctx context.Context, email, codigo string) (ResultadoConsulta, error) {
	p, err := s.localizar(ctx, email, codigo)
	if err != nil {
		return ResultadoConsulta{}, err
	}
	return s.resultadoDaConsulta(ctx, p)
}

// localizar acha o pedido do convidado por e-mail + código: código
// inexistente e e-mail errado passam pelo mesmo caminho (comparação em tempo
// constante contra o e-mail fantasma) e dão o mesmo ErrPedidoNaoEncontrado.
func (s *Servico) localizar(ctx context.Context, email, codigo string) (db.PedidoPorCodigoRow, error) {
	q := db.New(s.pool)
	codigo = normalizarCodigo(codigo)
	var linhas []db.PedidoPorCodigoRow
	if codigoValido(codigo) {
		var err error
		if linhas, err = q.PedidoPorCodigo(ctx, codigo); err != nil {
			return db.PedidoPorCodigoRow{}, fmt.Errorf("pedido: consulta: %w", err)
		}
	}
	alvo := emailFantasma
	if len(linhas) == 1 {
		alvo = linhas[0].Email
	}
	a := sha256.Sum256([]byte(normalizarEmail(email)))
	b := sha256.Sum256([]byte(normalizarEmail(alvo)))
	if subtle.ConstantTimeCompare(a[:], b[:]) != 1 || len(linhas) != 1 {
		return db.PedidoPorCodigoRow{}, ErrPedidoNaoEncontrado
	}
	return linhas[0], nil
}

// resultadoDaConsulta monta a visão do convidado: links só de ingressos
// ativos de pedido pago.
func (s *Servico) resultadoDaConsulta(ctx context.Context, p db.PedidoPorCodigoRow) (ResultadoConsulta, error) {
	out := ResultadoConsulta{Pedido: Visao{ID: p.ID, Codigo: p.Codigo, SessaoID: p.SessaoID, Assentos: p.Assentos,
		TotalCentavos: p.TotalCentavos, Status: Status(p.Status), ExpiraEm: p.ExpiraEm}}
	if out.Pedido.Status != Pago {
		return out, nil
	}
	vs := []Visao{out.Pedido}
	if err := s.marcarCancelaveis(ctx, vs); err != nil {
		return ResultadoConsulta{}, err
	}
	out.Pedido = vs[0]
	ingressos, err := db.New(s.pool).IngressosAtivosDoPedido(ctx, p.ID)
	if err != nil {
		return ResultadoConsulta{}, fmt.Errorf("pedido: ingressos da consulta: %w", err)
	}
	for _, i := range ingressos {
		segredo, ok := s.cfg.SegredosToken[i.VersaoToken]
		if !ok {
			return ResultadoConsulta{}, fmt.Errorf("pedido: sem segredo para a versão %d do token", i.VersaoToken)
		}
		out.Ingressos = append(out.Ingressos, IngressoDaConsulta{Assento: i.AssentoCodigo, Ref: i.ID.String() + "." + TokenIngresso(segredo, i.ID)})
	}
	return out, nil
}

// separarRef faz o parse estrito de "{uuid canônico}.{token de 43 chars}".
func separarRef(ref string) (uuid.UUID, string, bool) {
	idTexto, token, ok := strings.Cut(ref, ".")
	if !ok || len(idTexto) != 36 || len(token) != tamanhoToken {
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(idTexto)
	if err != nil || id.String() != idTexto {
		return uuid.Nil, "", false
	}
	for _, r := range token {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return uuid.Nil, "", false
		}
	}
	return id, token, true
}

// Ingresso valida o link público. O HMAC é calculado sempre (inclusive para
// id inexistente, com o segredo da versão 1) e comparado com hmac.Equal;
// expiração e revogação só são reveladas depois do token conferido.
func (s *Servico) Ingresso(ctx context.Context, ref string) (IngressoPublico, error) {
	id, token, formatoOK := separarRef(ref)
	var linhas []db.IngressoParaPaginaRow
	if formatoOK {
		var err error
		if linhas, err = db.New(s.pool).IngressoParaPagina(ctx, id); err != nil {
			return IngressoPublico{}, fmt.Errorf("pedido: ingresso: %w", err)
		}
	}
	versao := int16(1)
	if len(linhas) == 1 {
		versao = linhas[0].VersaoToken
	}
	segredo, ok := s.cfg.SegredosToken[versao]
	if !ok {
		// Versão aposentada/sem segredo: mesmo caminho (HMAC com a v1 → 404),
		// nunca um 500 que revelaria que o id existe (auditoria 0034).
		s.logger.Warn("pedido: ingresso com versão de token sem segredo", zap.Int16("versao", versao))
		segredo = s.cfg.SegredosToken[1]
		ok = false
	}
	esperado := TokenIngresso(segredo, id)
	if !hmac.Equal([]byte(esperado), []byte(token)) || !ok || !formatoOK || len(linhas) != 1 {
		return IngressoPublico{}, ErrIngressoInvalido
	}
	i := linhas[0]
	if i.Status == "cancelado" || Status(i.StatusPedido) != Pago {
		return IngressoPublico{}, ErrIngressoIndisponivel
	}
	info, err := s.cfg.Consulta.Sessao(ctx, i.SessaoID)
	if err != nil {
		return IngressoPublico{}, fmt.Errorf("pedido: sessão do ingresso: %w", err)
	}
	if s.cfg.Agora().After(info.Inicio.Add(ValidadeIngresso)) {
		return IngressoPublico{}, ErrIngressoIndisponivel
	}
	return IngressoPublico{Assento: i.AssentoCodigo, Status: i.Status, InfoSessao: info}, nil
}

// QRDoIngresso valida o link e gera o PNG com o mesmo link do e-mail.
func (s *Servico) QRDoIngresso(ctx context.Context, ref string) ([]byte, error) {
	if _, err := s.Ingresso(ctx, ref); err != nil {
		return nil, err
	}
	png, err := s.cfg.Consulta.QR(strings.TrimRight(s.cfg.Consulta.BaseURL, "/") + "/i/" + ref)
	if err != nil {
		return nil, fmt.Errorf("pedido: QR do ingresso: %w", err)
	}
	return png, nil
}
